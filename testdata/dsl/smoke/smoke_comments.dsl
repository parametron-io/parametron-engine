dsl v1.0
// Smoke test for DSL comments
const BASE = 10

profile Dev {
  output_dir = "smoke_comments" // profile setting comment
}

use profile Dev

product Commented {
  /* block comment before parameter */
  param x: number = BASE /* inline block */ + 2
  param y: number = x / 2 // division should still parse
  param endpoint: string = "http://example.test/a/*b*/c"
}
