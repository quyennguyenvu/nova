package generator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/quyennguyenvu/nova/internal/manifest"
)

const (
	enginePostgres = "postgres"
	engineMySQL    = "mysql"
)

// entityField is one exported field parsed from the domain entity struct.
type entityField struct {
	Name   string // Go field name, e.g. "CreatedAt"
	GoType string // Go type as written, e.g. "time.Time", "json.RawMessage", "string"
	Column string // snake_case column, e.g. "created_at"
}

// fieldMapping is an entityField resolved against an engine: the SQL column
// type plus the expressions to read it from / write it to the sqlc dbgen row.
type fieldMapping struct {
	entityField

	sqlType   string // engine column type, e.g. "TEXT", "TIMESTAMP WITH TIME ZONE"
	readExpr  string // RHS of `entity.X = <readExpr>`; the dbgen row var is "row"
	writeExpr string // RHS of `Params{X: <writeExpr>}`; the entity var is "e"
	pgTime    bool   // postgres timestamptz — mapper needs the pgtype import
}

// generateSQLCRepository produces the full sqlc-backed persistence slice for an
// existing entity: a migration, a query file, the repository impl, and the
// entity<->row mapper. It requires the entity to already exist (its fields drive
// the columns) and only runs when the project's stack uses sqlc + a SQL engine.
func (g *ComponentGenerator) generateSQLCRepository(name, engine string) error {
	title := toTitle(name)

	entityRes, ok := g.manifest.Resolve("entity", name, "")
	if !ok {
		return errors.New("manifest has no 'entity' layout entry")
	}
	entityPath := g.path(entityRes, "")
	fields, err := parseEntityStruct(entityPath, title)
	if err != nil {
		return err
	}

	cols, skipped := mapFields(engine, fields)
	for _, s := range skipped {
		fmt.Fprintf(
			os.Stdout,
			"   ⚠️  skipped field %s: unsupported type %q (add the column manually)\n",
			s.Name,
			s.GoType,
		)
	}
	if !hasColumn(cols, "id") {
		return fmt.Errorf("entity %s must have an `ID int64` field to back a repository", title)
	}

	repoRes, _ := g.manifest.Resolve("repository", name, engine)
	portRes, _ := g.manifest.Resolve("port", name, "")
	migRes, _ := g.manifest.Resolve("migration", name, "")
	queryRes, _ := g.manifest.Resolve("query", name, "")

	table := plural(snakeCase(name))
	insertCols := filterColumns(cols, "id")
	updateCols := filterColumns(insertCols, "created_at")

	mod := g.manifest.Module
	ic := implContext{
		engine: engine, title: title, lower: strings.ToLower(name), table: table,
		dbgenImport:  mod + "/" + repoRes.Dir + "/dbgen",
		mapperImport: mod + "/" + repoRes.Dir + "/mapper",
		domainImport: mod + "/" + portRes.Dir,
		entityImport: mod + "/" + entityRes.Dir,
		errorsImport: mod + "/pkg/errors",
	}

	ts := time.Now().Format("20060102150405")
	// rel paths are relative to baseDir — the write loop joins baseDir once.
	files := []genFile{
		{
			filepath.Join(migRes.Dir, fmt.Sprintf("%s_create_%s_table.up.sql", ts, table)),
			genMigrationUp(engine, table, insertCols),
			false,
		},
		{
			filepath.Join(migRes.Dir, fmt.Sprintf("%s_create_%s_table.down.sql", ts, table)),
			genMigrationDown(table),
			false,
		},
		{relOf(queryRes, ""), genQuery(engine, title, table, insertCols, updateCols), false},
		{relOf(repoRes, ""), genRepoImpl(ic), true},
		{
			filepath.Join(repoRes.Dir, "mapper", snakeCase(name)+".go"),
			genMapper(ic, cols, insertCols, updateCols),
			true,
		},
	}

	for _, f := range files {
		content := f.content
		if f.goCode {
			formatted, fmtErr := format.Source([]byte(content))
			if fmtErr != nil {
				return fmt.Errorf("generated invalid Go for %s: %w", f.rel, fmtErr)
			}
			content = string(formatted)
		}
		if wErr := writeFile(filepath.Join(g.baseDir, f.rel), content); wErr != nil {
			return wErr
		}
		fmt.Fprintf(os.Stdout, "   📄 %s\n", f.rel)
	}

	fmt.Fprintf(os.Stdout, "✅ Generated sqlc repository: %sRepository (%s)\n", title, engine)
	fmt.Fprintf(os.Stdout, "   ▶ run `make gen` (sqlc) then wire %sRepository into your DI provider\n", title)
	return nil
}

type genFile struct {
	rel     string
	content string
	goCode  bool
}

// implContext carries everything the Go templates below need.
type implContext struct {
	engine, title, lower, table                                         string
	dbgenImport, mapperImport, domainImport, entityImport, errorsImport string
}

// --- entity parsing -------------------------------------------------------

func parseEntityStruct(path, structName string) ([]entityField, error) {
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, fmt.Errorf(
			"entity %s not found at %s — run `nova add entity %s` first",
			structName, path, structName,
		)
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse entity %s: %w", path, err)
	}

	var fields []entityField
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		ts, isType := n.(*ast.TypeSpec)
		if !isType || ts.Name.Name != structName {
			return true
		}
		st, isStruct := ts.Type.(*ast.StructType)
		if !isStruct {
			return true
		}
		found = true
		for _, f := range st.Fields.List {
			typ := exprToTypeString(f.Type)
			for _, nm := range f.Names {
				if nm.IsExported() {
					fields = append(fields, entityField{Name: nm.Name, GoType: typ, Column: snakeCase(nm.Name)})
				}
			}
		}
		return false
	})
	if !found {
		return nil, fmt.Errorf(
			"struct %s not found in %s — run `nova add entity %s` first",
			structName, path, structName,
		)
	}
	return fields, nil
}

// exprToTypeString renders the supported subset of type expressions to source
// text: identifiers (string, int64), qualified names (time.Time), and slices
// ([]byte). Anything else returns "" and the field is skipped + warned.
func exprToTypeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprToTypeString(t.Elt)
		}
	}
	return ""
}

// --- type mapping ---------------------------------------------------------

func mapFields(engine string, fields []entityField) ([]fieldMapping, []entityField) {
	var cols []fieldMapping
	var skipped []entityField
	for _, f := range fields {
		fm, ok := mapField(engine, f)
		if !ok {
			skipped = append(skipped, f)
			continue
		}
		cols = append(cols, fm)
	}
	return cols, skipped
}

func mapField(engine string, f entityField) (fieldMapping, bool) {
	pg := engine == enginePostgres
	fm := fieldMapping{entityField: f, readExpr: "row." + f.Name, writeExpr: "e." + f.Name}
	switch f.GoType {
	case "int64":
		fm.sqlType = "BIGINT"
	case "int32":
		fm.sqlType = pick(pg, "INTEGER", "INT")
	case "int":
		fm.sqlType = "BIGINT"
		fm.readExpr = "int(row." + f.Name + ")"
		fm.writeExpr = "int64(e." + f.Name + ")"
	case "int16":
		fm.sqlType = "SMALLINT"
	case "string":
		fm.sqlType = pick(pg, "TEXT", "VARCHAR(255)")
	case "bool":
		fm.sqlType = pick(pg, "BOOLEAN", "TINYINT(1)")
	case "float64":
		fm.sqlType = pick(pg, "DOUBLE PRECISION", "DOUBLE")
	case "float32":
		fm.sqlType = pick(pg, "REAL", "FLOAT")
	case "time.Time":
		fm.sqlType = pick(pg, "TIMESTAMP WITH TIME ZONE", "DATETIME")
		if pg {
			fm.readExpr = "row." + f.Name + ".Time"
			fm.writeExpr = "pgtype.Timestamptz{Time: e." + f.Name + ", Valid: true}"
			fm.pgTime = true
		}
	case "json.RawMessage":
		fm.sqlType = pick(pg, "JSONB", "JSON")
	case "[]byte":
		fm.sqlType = pick(pg, "BYTEA", "BLOB")
	default:
		return fieldMapping{}, false
	}
	return fm, true
}

// --- migration ------------------------------------------------------------

func genMigrationUp(engine, table string, insertCols []fieldMapping) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n", table)
	if engine == enginePostgres {
		b.WriteString("    id BIGSERIAL PRIMARY KEY")
	} else {
		b.WriteString("    id BIGINT AUTO_INCREMENT PRIMARY KEY")
	}
	for _, c := range insertCols {
		def := ""
		if c.Column == "created_at" || c.Column == "updated_at" {
			def = pick(engine == enginePostgres, " DEFAULT NOW()", " DEFAULT CURRENT_TIMESTAMP")
		}
		fmt.Fprintf(&b, ",\n    %s %s NOT NULL%s", c.Column, c.sqlType, def)
	}
	b.WriteString("\n);\n")
	return b.String()
}

func genMigrationDown(table string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;\n", table)
}

// --- sqlc query -----------------------------------------------------------

func genQuery(engine, title, table string, insertCols, updateCols []fieldMapping) string {
	pg := engine == enginePostgres
	var b strings.Builder

	// Create
	colNames := columnNames(insertCols)
	if pg {
		fmt.Fprintf(&b, "-- name: Create%s :one\nINSERT INTO %s (%s)\nVALUES (%s)\nRETURNING *;\n\n",
			title, table, strings.Join(colNames, ", "), pgPlaceholders(1, len(colNames)))
	} else {
		fmt.Fprintf(&b, "-- name: Create%s :execlastid\nINSERT INTO %s (%s)\nVALUES (%s);\n\n",
			title, table, strings.Join(colNames, ", "), qmPlaceholders(len(colNames)))
	}

	// GetByID
	fmt.Fprintf(&b, "-- name: Get%sByID :one\nSELECT * FROM %s\nWHERE id = %s;\n\n",
		title, table, pick(pg, "$1", "?"))

	// Update
	b.WriteString(genUpdateQuery(pg, title, table, updateCols))

	// Delete
	fmt.Fprintf(&b, "-- name: Delete%s :exec\nDELETE FROM %s\nWHERE id = %s;\n\n",
		title, table, pick(pg, "$1", "?"))

	// List
	if pg {
		fmt.Fprintf(
			&b,
			"-- name: List%ss :many\nSELECT * FROM %s\nORDER BY id\nLIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::int;\n",
			title,
			table,
		)
	} else {
		fmt.Fprintf(&b, "-- name: List%ss :many\nSELECT * FROM %s\nORDER BY id\nLIMIT ? OFFSET ?;\n", title, table)
	}
	return b.String()
}

func genUpdateQuery(pg bool, title, table string, updateCols []fieldMapping) string {
	sets := make([]string, len(updateCols))
	for i, c := range updateCols {
		sets[i] = fmt.Sprintf("%s = %s", c.Column, pick(pg, fmt.Sprintf("$%d", i+1), "?"))
	}
	where := pick(pg, fmt.Sprintf("$%d", len(updateCols)+1), "?")
	return fmt.Sprintf("-- name: Update%s :exec\nUPDATE %s SET %s\nWHERE id = %s;\n\n",
		title, table, strings.Join(sets, ", "), where)
}

// --- repository impl ------------------------------------------------------

func genRepoImpl(ic implContext) string {
	pg := ic.engine == enginePostgres
	imports := repoImports(ic, pg)
	ctor := pick(pg, "pool *pgxpool.Pool", "db *sql.DB")
	ctorArg := pick(pg, "pool", "db")
	noRows := pick(pg, "pgx.ErrNoRows", "sql.ErrNoRows")

	var create string
	if pg {
		create = fmt.Sprintf(`	row, err := r.qx.Q(ctx).Create%s(ctx, mapper.%sToCreateParams(e))
	if err != nil {
		return errors.Wrapf(err, "%s repo: insert")
	}
	e.ID = row.ID
	return nil`, ic.title, ic.title, ic.lower)
	} else {
		create = fmt.Sprintf(`	id, err := r.qx.Q(ctx).Create%s(ctx, mapper.%sToCreateParams(e))
	if err != nil {
		return errors.Wrapf(err, "%s repo: insert")
	}
	e.ID = id
	return nil`, ic.title, ic.title, ic.lower)
	}

	return fmt.Sprintf(`package %s

%s

var _ domain.%sRepository = (*%sRepository)(nil)

// %sRepository implements domain.%sRepository using sqlc-generated queries.
// Row<->entity hydration goes through the mapper package.
type %sRepository struct {
	qx *qx
}

func New%sRepository(%s) *%sRepository {
	return &%sRepository{qx: newQX(%s)}
}

func (r *%sRepository) Create(ctx context.Context, e *entity.%s) error {
%s
}

func (r *%sRepository) GetByID(ctx context.Context, id int64) (*entity.%s, error) {
	row, err := r.qx.Q(ctx).Get%sByID(ctx, id)
	if err != nil {
		if stderrors.Is(err, %s) {
			return nil, errors.Wrapf(errors.ErrNotFound, "%s repo: select id=%%d", id)
		}
		return nil, errors.Wrapf(err, "%s repo: select id=%%d", id)
	}
	return mapper.%sToEntity(row), nil
}

func (r *%sRepository) Update(ctx context.Context, e *entity.%s) error {
	if err := r.qx.Q(ctx).Update%s(ctx, mapper.%sToUpdateParams(e)); err != nil {
		return errors.Wrapf(err, "%s repo: update id=%%d", e.ID)
	}
	return nil
}

func (r *%sRepository) Delete(ctx context.Context, id int64) error {
	if err := r.qx.Q(ctx).Delete%s(ctx, id); err != nil {
		return errors.Wrapf(err, "%s repo: delete id=%%d", id)
	}
	return nil
}

func (r *%sRepository) List(ctx context.Context, limit, offset int32) ([]*entity.%s, error) {
	rows, err := r.qx.Q(ctx).List%ss(ctx, dbgen.List%ssParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "%s repo: list")
	}
	return mapper.%ssToEntities(rows), nil
}
`,
		ic.engine, imports,
		ic.title, ic.title,
		ic.title, ic.title,
		ic.title,
		ic.title, ctor, ic.title, ic.title, ctorArg,
		ic.title, ic.title, create,
		ic.title, ic.title, ic.title, noRows, ic.lower, ic.lower, ic.title,
		ic.title, ic.title, ic.title, ic.title, ic.lower,
		ic.title, ic.title, ic.lower,
		ic.title, ic.title, ic.title, ic.title, ic.lower, ic.title,
	)
}

func repoImports(ic implContext, pg bool) string {
	std := "\t\"context\"\n\tstderrors \"errors\""
	if !pg {
		std = "\t\"context\"\n\t\"database/sql\"\n\tstderrors \"errors\""
	}
	var third string
	if pg {
		third = "\n\n\t\"github.com/jackc/pgx/v5\"\n\t\"github.com/jackc/pgx/v5/pgxpool\""
	}
	internal := fmt.Sprintf("\n\n\t\"%s\"\n\t\"%s\"\n\t\"%s\"\n\t\"%s\"\n\t\"%s\"",
		ic.dbgenImport, ic.mapperImport, ic.domainImport, ic.entityImport, ic.errorsImport)
	return "import (\n" + std + third + internal + "\n)"
}

// --- mapper ---------------------------------------------------------------

func genMapper(ic implContext, allCols, insertCols, updateCols []fieldMapping) string {
	pg := ic.engine == enginePostgres
	needPgtype := false
	for _, c := range insertCols {
		if c.pgTime {
			needPgtype = true
		}
	}

	imports := fmt.Sprintf("\t\"%s\"\n\t\"%s\"", ic.dbgenImport, ic.entityImport)
	if pg && needPgtype {
		imports = "\t\"github.com/jackc/pgx/v5/pgtype\"\n\n" + imports
	}

	return fmt.Sprintf(`// Package mapper converts between domain entities and sqlc-generated DB rows.
// One file per entity. All functions are pure — no DB handles, no context.
package mapper

import (
%s
)

// %sToEntity maps a sqlc-generated row into the domain entity.
func %sToEntity(row dbgen.%s) *entity.%s {
	return &entity.%s{
%s
	}
}

// %ssToEntities maps a batch of rows.
func %ssToEntities(rows []dbgen.%s) []*entity.%s {
	out := make([]*entity.%s, len(rows))
	for i, r := range rows {
		out[i] = %sToEntity(r)
	}
	return out
}

// %sToCreateParams builds the sqlc insert params from the entity.
func %sToCreateParams(e *entity.%s) dbgen.Create%sParams {
	return dbgen.Create%sParams{
%s
	}
}

// %sToUpdateParams builds the sqlc update params from the entity.
func %sToUpdateParams(e *entity.%s) dbgen.Update%sParams {
	return dbgen.Update%sParams{
%s
		ID: e.ID,
	}
}
`,
		imports,
		ic.title, ic.title, ic.title, ic.title, ic.title, structAssign(allCols, "\t\t", true),
		ic.title, ic.title, ic.title, ic.title, ic.title, ic.title,
		ic.title, ic.title, ic.title, ic.title, ic.title, structAssign(insertCols, "\t\t", false),
		ic.title, ic.title, ic.title, ic.title, ic.title, structAssign(updateCols, "\t\t", false),
	)
}

// structAssign renders one `Field: <expr>,` line per column. When read is true
// it builds the row->entity direction (entity field = readExpr); otherwise the
// entity->params direction (param field = writeExpr).
func structAssign(cols []fieldMapping, indent string, read bool) string {
	var b strings.Builder
	for _, c := range cols {
		expr := c.writeExpr
		if read {
			expr = c.readExpr
		}
		fmt.Fprintf(&b, "%s%s: %s,\n", indent, c.Name, expr)
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- small helpers --------------------------------------------------------

func pick(cond bool, ifTrue, ifFalse string) string {
	if cond {
		return ifTrue
	}
	return ifFalse
}

func plural(s string) string { return s + "s" }

// relOf returns a resolved target's path relative to the project root (Dir +
// File), using fallback when the layout entry has no File pattern.
func relOf(res manifest.Resolved, fallback string) string {
	file := res.File
	if file == "" {
		file = fallback
	}
	return filepath.Join(res.Dir, file)
}

func hasColumn(cols []fieldMapping, col string) bool {
	for _, c := range cols {
		if c.Column == col {
			return true
		}
	}
	return false
}

func filterColumns(cols []fieldMapping, exclude string) []fieldMapping {
	out := make([]fieldMapping, 0, len(cols))
	for _, c := range cols {
		if c.Column != exclude {
			out = append(out, c)
		}
	}
	return out
}

func columnNames(cols []fieldMapping) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Column
	}
	return out
}

func pgPlaceholders(start, n int) string {
	out := make([]string, n)
	for i := range n {
		out[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(out, ", ")
}

func qmPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// snakeCase converts a Go identifier to snake_case, keeping acronyms intact
// (ID -> id, UserID -> user_id, HTTPServer -> http_server).
func snakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
