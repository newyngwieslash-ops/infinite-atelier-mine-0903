package files

import (
	"context"
	"io"
)

// Service imports bytes through Store, then records metadata through Repository.
type Service struct {
	store Store
	repo  Repository
}

// NewService constructs an importer. Both dependencies are required.
func NewService(store Store, repo Repository) *Service {
	return &Service{store: store, repo: repo}
}

// Import stores file bytes and upserts metadata. Display names are never paths.
func (s *Service) Import(ctx context.Context, displayName string, body io.Reader) (Object, error) {
	object, err := s.store.Put(ctx, displayName, body)
	if err != nil {
		return Object{}, err
	}
	if err := s.repo.UpsertObject(ctx, object); err != nil {
		return Object{}, err
	}
	return object, nil
}
