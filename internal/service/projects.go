package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func (s *AMMService) RegisterProject(ctx context.Context, project *core.Project) (*core.Project, error) {
	projectID := ""
	projectName := ""
	if project != nil {
		projectID = project.ID
		projectName = project.Name
	}
	slog.Debug("RegisterProject called", "id", projectID, "name", projectName)

	if project == nil {
		return nil, fmt.Errorf("%w: project is required", core.ErrInvalidInput)
	}

	if project.ID == "" {
		project.ID = core.GenerateID("prj_")
	}
	now := time.Now().UTC()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now

	if err := s.repo.InsertProject(ctx, project); err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
	return project, nil
}

func (s *AMMService) GetProject(ctx context.Context, id string) (*core.Project, error) {
	slog.Debug("GetProject called", "id", id)
	return s.repo.GetProject(ctx, id)
}

func (s *AMMService) ListProjects(ctx context.Context) ([]core.Project, error) {
	slog.Debug("ListProjects called")
	return s.repo.ListProjects(ctx)
}

func (s *AMMService) RemoveProject(ctx context.Context, id string) error {
	slog.Debug("RemoveProject called", "id", id)
	if err := s.repo.DeleteProject(ctx, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (s *AMMService) AddRelationship(ctx context.Context, rel *core.Relationship) (*core.Relationship, error) {
	relID := ""
	fromEntityID := ""
	toEntityID := ""
	relType := ""
	if rel != nil {
		relID = rel.ID
		fromEntityID = rel.FromEntityID
		toEntityID = rel.ToEntityID
		relType = rel.RelationshipType
	}
	slog.Debug("AddRelationship called", "id", relID, "from_entity_id", fromEntityID, "to_entity_id", toEntityID, "relationship_type", relType)

	if rel.ID == "" {
		rel.ID = core.GenerateID("rel_")
	}
	now := time.Now().UTC()
	if rel.CreatedAt.IsZero() {
		rel.CreatedAt = now
	}
	rel.UpdatedAt = now

	if err := s.repo.InsertRelationship(ctx, rel); err != nil {
		return nil, fmt.Errorf("insert relationship: %w", err)
	}
	return rel, nil
}

func (s *AMMService) GetRelationship(ctx context.Context, id string) (*core.Relationship, error) {
	slog.Debug("GetRelationship called", "id", id)
	return s.repo.GetRelationship(ctx, id)
}

func (s *AMMService) ListRelationships(ctx context.Context, opts core.ListRelationshipsOptions) ([]core.Relationship, error) {
	slog.Debug("ListRelationships called", "entity_id", opts.EntityID, "relationship_type", opts.RelationshipType, "limit", opts.Limit)
	return s.repo.ListRelationships(ctx, opts)
}

func (s *AMMService) RemoveRelationship(ctx context.Context, id string) error {
	slog.Debug("RemoveRelationship called", "id", id)
	if err := s.repo.DeleteRelationship(ctx, id); err != nil {
		return fmt.Errorf("delete relationship: %w", err)
	}
	return nil
}
