package store

import (
	"context"
	"errors"
	"testing"

	"app/internal/brand"
)

func TestBrandLogo(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	owner, err := st.CreatePerson(ctx, "logo-owner", "Logo Owner", "logo-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	cid, err := st.CreateCommunity(ctx, "Studio", owner)
	if err != nil {
		t.Fatal(err)
	}
	bid, err := st.CreateBand(ctx, "Band", owner)
	if err != nil {
		t.Fatal(err)
	}

	logo := brand.LogoImage{Data: []byte("\x89PNG fake"), ContentType: "image/png", Width: 10, Height: 10, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := st.SetBrand(ctx, "community", cid, brand.Brand{Mark: "S", LogoURL: "https://example.com/l.png", LogoImage: "/brand-logos/community/99/ffffffffffffffff.png"}); err != nil {
		t.Fatal(err)
	}
	b, _, err := st.GetBrand(ctx, "community", cid)
	if err != nil {
		t.Fatal(err)
	}
	if b.LogoImage != "" || b.LogoURL != "https://example.com/l.png" {
		t.Fatalf("a logoImage saved with the brand: %+v", b)
	}

	if err := st.SetBrandLogo(ctx, "community", cid, logo, owner); err != nil {
		t.Fatal(err)
	}
	b, _, _ = st.GetBrand(ctx, "community", cid)
	want := brand.LogoPath("community", cid, logo.SHA256, logo.ContentType)
	if b.LogoImage != want || b.Mark != "S" || b.Logo() != want {
		t.Fatalf("got %+v, want logoImage %s", b, want)
	}
	// Saving the form again keeps the uploaded logo.
	if err := st.SetBrand(ctx, "community", cid, brand.Brand{Mark: "T"}); err != nil {
		t.Fatal(err)
	}
	if b, _, _ = st.GetBrand(ctx, "community", cid); b.LogoImage != want {
		t.Fatalf("the logo went with a brand save: %+v", b)
	}
	// The band has none of it.
	if bb, _, _ := st.GetBrand(ctx, "band", bid); bb.LogoImage != "" {
		t.Fatalf("band got the community's logo: %+v", bb)
	}

	// Replaced: one row, the new image.
	logo2 := logo
	logo2.Data, logo2.ContentType, logo2.SHA256 = []byte("jpeg"), "image/jpeg", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	if err := st.SetBrandLogo(ctx, "community", cid, logo2, 0); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetBrandLogo(ctx, "community", cid)
	if err != nil || string(got.Data) != "jpeg" || got.ContentType != "image/jpeg" || got.SHA256 != logo2.SHA256 {
		t.Fatalf("GetBrandLogo: %+v %v", got, err)
	}
	var n int
	st.db.QueryRow(ctx, `SELECT count(*) FROM brand_logos`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d logos, want 1", n)
	}

	if err := st.SetBrandLogo(ctx, "band", 999999, logo, owner); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no such band: %v", err)
	}
	if _, err := st.GetBrandLogo(ctx, "band", bid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("band without a logo: %v", err)
	}

	if err := st.DeleteBrandLogo(ctx, "community", cid); err != nil {
		t.Fatal(err)
	}
	if b, _, _ = st.GetBrand(ctx, "community", cid); b.LogoImage != "" {
		t.Fatalf("still there: %+v", b)
	}

	// Deleting the band deletes its logo.
	if err := st.SetBrandLogo(ctx, "band", bid, logo, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `DELETE FROM bands WHERE id = $1`, bid); err != nil {
		t.Fatal(err)
	}
	st.db.QueryRow(ctx, `SELECT count(*) FROM brand_logos`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d logos left", n)
	}
}
