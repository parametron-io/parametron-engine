# Real FreeCAD technical drawing capture fixtures

These project fixtures are verbatim snapshots of the committed
`parametron-freecad` technical drawing capture fixtures produced by real
FreeCAD and accepted by the Engine CLI in
`parametron-io/parametron-freecad#93` / `#97`.

- `single-sheet`, `multi-sheet`, and `multiple-drawings` contain the committed
  real capture `prm.cad.json`, its `prm.semantic-map.json`, project DSL, and
  `prm.project.json`.
- `input/model.FCStd` is the corresponding committed FreeCAD source fixture
  (`single-page.FCStd` or `multiple-pages.FCStd`) copied to the
  project-relative path declared by `prm.project.json`.

They are consumed by explicit Engine acceptance tests only and are not
automatically scanned by the DSL smoke or break matrices.
