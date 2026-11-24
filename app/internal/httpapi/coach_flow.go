package httpapi

import (
	"context"

	"app/internal/store"
)

// coachingSongFor is the song a coaching community's songbook sb gets
// when variantID is added to it: the community's own version of it when
// it has one ("Make the community's version", store.MakeVersion), so the
// coach and the students keep working on one song; else variantID.
// Any other songbook gets variantID.
func (s *Server) coachingSongFor(ctx context.Context, sb *store.Songbook, variantID int64) int64 {
	if sb == nil || sb.OwnerKind != "community" {
		return variantID
	}
	c, err := s.store.GetCommunity(ctx, sb.OwnerCommunityID)
	if err != nil || !c.IsCoaching() {
		return variantID
	}
	id, err := s.store.CommunityVersionOf(ctx, c.ID, variantID)
	if err != nil {
		return variantID
	}
	return id
}
