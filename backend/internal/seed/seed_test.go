package seed_test

import (
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/repo/db"
	"github.com/Baiel1922/zhim-back/backend/internal/seed"
	"github.com/Baiel1922/zhim-back/backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

func TestSeedIsIdempotent(t *testing.T) {
	t.Parallel()
	pool := pgtest.NewPool(t)
	store := repo.NewStore(pool)
	ctx := t.Context()

	for run := 1; run <= 2; run++ {
		res, err := seed.Run(ctx, store, seed.Options{OwnerTelegramID: 424242, OwnerName: "Owner"})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if !res.OwnerSeeded {
			t.Fatalf("run %d: owner not seeded", run)
		}
	}

	counts := map[string]int{
		"categories": 3, "colors": 3, "products": 6, "product_variants": 60,
		"size_charts": 1, "delivery_zones": 2, "settings": 3, "admins": 1,
	}
	for table, want := range counts {
		var got int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: %d rows, want %d", table, got, want)
		}
	}

	owner, err := store.GetActiveAdminByTelegramID(ctx, 424242)
	if err != nil {
		t.Fatal(err)
	}
	if owner.Role != db.AdminRoleOwner {
		t.Errorf("owner role = %s", owner.Role)
	}

	products, err := store.ListActiveProducts(ctx, db.ListActiveProductsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 6 {
		t.Fatalf("active products = %d", len(products))
	}
	for _, p := range products {
		if p.TitleKy == "" || len(p.AvailableSizes) == 0 || len(p.AvailableColorIDs) == 0 {
			t.Errorf("incomplete product %s: %+v", p.Slug, p)
		}
	}
}

func TestSeedWithoutOwner(t *testing.T) {
	t.Parallel()
	store := repo.NewStore(pgtest.NewPool(t))

	res, err := seed.Run(t.Context(), store, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnerSeeded {
		t.Fatal("owner must be skipped without a telegram id")
	}
	admins, err := store.ListActiveAdmins(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(admins) != 0 {
		t.Fatalf("admins = %d", len(admins))
	}
}
