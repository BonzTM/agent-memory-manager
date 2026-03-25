package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func (s *AMMService) RegisterProject(ctx context.Context, project *core.Project) (*core.Project, error) {
	if project.ID == "" {
		project.ID = generateID("prj_")
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
	return s.repo.GetProject(ctx, id)
}

func (s *AMMService) ListProjects(ctx context.Context) ([]core.Project, error) {
	return s.repo.ListProjects(ctx)
}

func (s *AMMService) RemoveProject(ctx context.Context, id string) error {
	if err := s.repo.DeleteProject(ctx, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (s *AMMService) AddRelationship(ctx context.Context, rel *core.Relationship) (*core.Relationship, error) {
	if rel.ID == "" {
		rel.ID = generateID("rel_")
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
	return s.repo.GetRelationship(ctx, id)
}

func (s *AMMService) ListRelationships(ctx context.Context, opts core.ListRelationshipsOptions) ([]core.Relationship, error) {
	return s.repo.ListRelationships(ctx, opts)
}

func (s *AMMService) RemoveRelationship(ctx context.Context, id string) error {
	if err := s.repo.DeleteRelationship(ctx, id); err != nil {
		return fmt.Errorf("delete relationship: %w", err)
	}
	return nil
}
