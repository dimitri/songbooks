package webassets

import "app/internal/brand"

// BrandLookupFor is the look of the band or community id ("band" or
// "community"), nil for none: brandFor for the handlers that build a
// space's header (httpapi's space_nav.go).
func BrandLookupFor(kind string, id int64) *brand.Theme { return brandFor(kind, id) }
