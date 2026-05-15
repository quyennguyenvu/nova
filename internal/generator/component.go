package generator

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

// ComponentGenerator generates individual components in an existing project.
type ComponentGenerator struct {
	baseDir string
}

// NewComponentGenerator creates a component generator rooted at baseDir.
func NewComponentGenerator(baseDir string) *ComponentGenerator {
	return &ComponentGenerator{baseDir: baseDir}
}

// GenerateEntity creates a new entity file.
func (g *ComponentGenerator) GenerateEntity(name string) error {
	entityName := toTitle(name)
	lowerName := strings.ToLower(name)
	snakeName := toSnakeCase(name)

	content := fmt.Sprintf(`package entity

import "time"

type %s struct {
	ID        int64     `+"`json:\"id\"`"+`
	CreatedAt time.Time `+"`json:\"created_at\"`"+`
	UpdatedAt time.Time `+"`json:\"updated_at\"`"+`
}

func New%s() *%s {
	now := time.Now()
	return &%s{
		CreatedAt: now,
		UpdatedAt: now,
	}
}
`, entityName, entityName, entityName, entityName)

	outPath := filepath.Join(g.baseDir, "internal/domain/entity", snakeName+".go")
	if err := writeFile(outPath, content); err != nil {
		return err
	}

	// Also generate repository interface
	repoContent := fmt.Sprintf(`package repository

import (
	"context"
)

type %sRepository interface {
	Create(ctx context.Context, %s interface{}) error
	GetByID(ctx context.Context, id int64) (interface{}, error)
	Update(ctx context.Context, %s interface{}) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, limit, offset int) ([]interface{}, error)
}
`, entityName, lowerName, lowerName)

	repoPath := filepath.Join(g.baseDir, "internal/domain/repository", snakeName+"_repository.go")
	if err := writeFile(repoPath, repoContent); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "✅ Generated entity: %s\n", entityName)
	fmt.Fprintf(os.Stdout, "   📄 %s\n", outPath)
	fmt.Fprintf(os.Stdout, "   📄 %s\n", repoPath)

	return nil
}

// GenerateUseCase creates a new use case.
func (g *ComponentGenerator) GenerateUseCase(name string) error {
	lowerName := strings.ToLower(name)
	entityName := toTitle(name)
	dir := filepath.Join(g.baseDir, "internal/usecase", lowerName)

	// Service
	serviceContent := fmt.Sprintf(`package %s

import (
	"context"
)

type Service struct {
}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Create%s(ctx context.Context, input Create%sInput) (interface{}, error) {
	// TODO: implement
	return nil, nil
}

func (s *Service) Get%s(ctx context.Context, id int64) (interface{}, error) {
	// TODO: implement
	return nil, nil
}
`, lowerName, entityName, entityName, entityName)

	dtoContent := fmt.Sprintf(`package %s

type Create%sInput struct {
	// TODO: add fields
}

type Update%sInput struct {
	// TODO: add fields
}
`, lowerName, entityName, entityName)

	errorsContent := fmt.Sprintf(`package %s

import "errors"

var (
	Err%sNotFound      = errors.New("%s not found")
	Err%sAlreadyExists = errors.New("%s already exists")
)
`, lowerName, entityName, lowerName, entityName, lowerName)

	files := map[string]string{
		filepath.Join(dir, "service.go"): serviceContent,
		filepath.Join(dir, "dto.go"):     dtoContent,
		filepath.Join(dir, "errors.go"):  errorsContent,
	}

	for path, content := range files {
		if err := writeFile(path, content); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "   📄 %s\n", path)
	}

	fmt.Fprintf(os.Stdout, "✅ Generated use case: %s\n", lowerName)
	return nil
}

// GenerateHandler creates a new HTTP handler.
func (g *ComponentGenerator) GenerateHandler(name string) error {
	lowerName := strings.ToLower(name)
	entityName := toTitle(name)

	content := fmt.Sprintf(`package v1

import (
	"github.com/gofiber/fiber/v2"
)

type %sHandler struct {
	// TODO: inject %s use case service
}

func New%sHandler() *%sHandler {
	return &%sHandler{}
}

func (h *%sHandler) Create(c *fiber.Ctx) error {
	// TODO: implement
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "created"})
}

func (h *%sHandler) GetByID(c *fiber.Ctx) error {
	// TODO: implement
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *%sHandler) Update(c *fiber.Ctx) error {
	// TODO: implement
	return c.JSON(fiber.Map{"status": "updated"})
}

func (h *%sHandler) Delete(c *fiber.Ctx) error {
	// TODO: implement
	return c.SendStatus(fiber.StatusNoContent)
}
`, entityName, lowerName, entityName, entityName, entityName, entityName, entityName, entityName, entityName)

	outPath := filepath.Join(g.baseDir, "internal/adapter/handler/http/v1", lowerName+"_handler.go")
	if err := writeFile(outPath, content); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "✅ Generated handler: %sHandler\n", entityName)
	fmt.Fprintf(os.Stdout, "   📄 %s\n", outPath)
	return nil
}

// GenerateRepository creates a new repository implementation.
func (g *ComponentGenerator) GenerateRepository(name, repoType string) error {
	lowerName := strings.ToLower(name)
	entityName := toTitle(name)
	snakeName := toSnakeCase(name)

	content := fmt.Sprintf(`package %s

import (
	"context"
	"fmt"
)

type %sRepository struct {
	// TODO: inject database connection
}

func New%sRepository() *%sRepository {
	return &%sRepository{}
}

func (r *%sRepository) Create(ctx context.Context, %s interface{}) error {
	// TODO: implement
	return fmt.Errorf("not implemented")
}

func (r *%sRepository) GetByID(ctx context.Context, id int64) (interface{}, error) {
	// TODO: implement
	return nil, fmt.Errorf("not implemented")
}

func (r *%sRepository) Update(ctx context.Context, %s interface{}) error {
	// TODO: implement
	return fmt.Errorf("not implemented")
}

func (r *%sRepository) Delete(ctx context.Context, id int64) error {
	// TODO: implement
	return fmt.Errorf("not implemented")
}

func (r *%sRepository) List(ctx context.Context, limit, offset int) ([]interface{}, error) {
	// TODO: implement
	return nil, fmt.Errorf("not implemented")
}
`, repoType, entityName, entityName, entityName, entityName, entityName, lowerName, entityName, entityName, lowerName, entityName, entityName)

	outPath := filepath.Join(g.baseDir, "internal/adapter/repository", repoType, snakeName+"_repository.go")
	if err := writeFile(outPath, content); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "✅ Generated repository: %sRepository (%s)\n", entityName, repoType)
	fmt.Fprintf(os.Stdout, "   📄 %s\n", outPath)
	return nil
}

// Helper functions

func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func toTitle(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func toSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result = append(result, '_')
		}
		result = append(result, unicode.ToLower(r))
	}
	return string(result)
}

// Unused but useful template helper.
var _ = template.New
var _ = bytes.NewBuffer
