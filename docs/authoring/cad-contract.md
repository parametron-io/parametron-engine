# CAD Capture Contract

`parametron.cad.json` describes a captured CAD automation surface. Engine owns
the input schema and its interpretation: document identity, captured entities,
ownership, hierarchy, values, and capabilities available to authoring. Capture
production and CAD-native operations belong to the CAD integration.

This page is the schema reference. The
[target-action contract](../reference/target-action-contract.md) owns the complete
action, mutation, and runtime-handoff specification.

## Top-level object

The supported `schemaVersion` is exactly `"1.0"`. All fields below are required.
Unknown fields are rejected at every schema object level.

| Field | Shape and meaning |
| --- | --- |
| `schemaVersion` | Exact version string. |
| `captureId` | Nonempty capture identifier without leading/trailing whitespace. Engine consumes this identifier; it does not derive it from the contents. |
| `adapter` | Object with required `name` and `version` strings. |
| `cadSystem` | Object with required `name` and `version` strings. |
| `sourceDocument` | Object with required nonempty strings `logicalId`, `path`, and `fingerprint`. |
| `rootProduct` | Object with `id` referring to a captured component. |
| `annotations` | Required `description`, `comment`, and `purpose` strings; empty strings are valid. |
| `entities` | Six required entity arrays described below. |
| `structure` | Required `rootComponentId` and `nodes` describing component hierarchy. |

Adapter and CAD-system names must be nonblank. Their versions may be empty, but
whitespace-only versions are invalid. The source path and fingerprint are
descriptive input strings; the schema validator does not impose a checksum
algorithm or verify the source file's bytes.

`entities` requires `components`, `features`, `relationships`,
`parameterGroups`, `parameters`, and `metadata` arrays. Empty arrays are
accepted structurally, but a valid complete document needs an assembly component
for the root. `structure.nodes` must be nonempty. Required arrays cannot be
omitted or replaced with `null`.

## Entity fields

All fields listed as required must be present, including booleans whose value
is false. Every entity has nonblank `id` and `cadType` strings and a required
`annotations` object with the same three string fields as root annotations.
IDs are unique across all six registries, not just within each array.

### Components

Required fields: `id`, `kind`, `name`, `displayName`, `cadType`,
`quantity`, `material`, `targetability`, and `annotations`.

- `kind` is `"assembly"` or `"part"`.
- `name` is nonblank; `displayName` and `material` may be empty.
- `quantity` is a positive integer.
- Optional `sourceFile` is an object containing a required nonblank `path`.
- Optional `identitySource` and `stabilityClass` describe capture identity.

### Features

Required fields: `id`, `componentId`, `name`, `displayName`, `cadType`,
`targetability`, and `annotations`.

`componentId` must resolve to a component. `name` is nonblank and
`displayName` may be empty. `identitySource` and `stabilityClass` are optional.

### Relationships

Required fields: `id`, `kind`, `componentId`, `name`, `cadType`,
`endpoints`, `targetability`, and `annotations`.

`kind` is `"mate"` or `"relationship"`. The owning `componentId` must
resolve. `endpoints` is a nonempty array of unique component or feature IDs.
Endpoint order is preserved by canonical serialization. `identitySource` and
`stabilityClass` are optional. Relationships have no `displayName` field.

### Parameter groups

Required fields: `id`, `ownerComponentId`, `name`, `displayName`,
`groupKind`, `cadType`, `observable`, `writable`, and `annotations`.

`ownerComponentId` must resolve to a component. `groupKind` is `"varset"`,
`"design_table"`, or `"cad_native_group"`. `name` is nonblank;
`displayName` may be empty. `observable` and `writable` are booleans.
`identitySource` and `stabilityClass` are optional.

### Parameters

Required fields: `id`, `ownerKind`, `ownerId`, `componentId`, `name`,
`displayName`, `cadType`, `valueType`, `observable`, `writable`, and
`annotations`.

`ownerKind` is `"group"`, `"component"`, or `"feature"`; `ownerId` must
resolve to that entity kind. `componentId` must match the owner's component,
or equal `ownerId` for component ownership. `name` is nonblank;
`displayName` may be empty.

`valueType` is `"number"`, `"string"`, or `"boolean"`. Optional
`currentValue` must be a JSON scalar of the declared type; `null`, arrays,
and objects are invalid. Optional `unit` is a string that may be empty but not
whitespace-only. `identitySource` and `stabilityClass` are optional.

### Metadata

Required fields: `id`, `ownerKind`, `ownerId`, `componentId`, `key`,
`displayName`, `cadType`, `valueType`, `observable`, `writable`, and
`annotations`.

`ownerKind` is `"component"`, `"feature"`, or `"relationship"`.
`ownerId` must resolve, and `componentId` must agree with that owner.
`key` is nonblank; `displayName` may be empty. The scalar type and optional
`currentValue` rules match parameters. Metadata has no `unit` or `name`
field. `identitySource` and `stabilityClass` are optional.

## Identity and names

`id` is the captured stable reference; `name` is the captured machine-facing
name; `displayName` is descriptive text. Engine does not regenerate IDs from
names or use display labels as an identity fallback. References use exact,
case-sensitive IDs.

Optional `identitySource` contains a required nonblank `kind` and optional
string fields `path`, `ownerScopedKey`, `groupScopedKey`, and `nativeRef`.
When supplied, it has these entity-specific requirements:

| Entity | Required identity-source content |
| --- | --- |
| Component | `kind: "parent_scoped_path"` and nonblank `path`. |
| Feature, parameter group, metadata | `kind: "owner_scoped_key"` and nonblank `ownerScopedKey`. |
| Component- or feature-owned parameter | `kind: "owner_scoped_key"` and nonblank `ownerScopedKey`. |
| Group-owned parameter | `kind: "owner_scoped_key"`, nonblank `ownerScopedKey`, and nonblank `groupScopedKey`. |
| Relationship | Nonblank `kind`; the validator imposes no additional entity-specific key requirement. |

Duplicate component identity-source paths are rejected. Feature keys are checked
within their component scope, and group keys within their owner-component scope.
Parameter and metadata owner keys share a uniqueness check by owner kind and ID.
These checks validate supplied identity evidence; they do not compute an ID or
prove identity stability across separate captures.

Optional `stabilityClass` accepts `"stable"`, `"conditionally_stable"`,
`"unstable"`, or an empty string.

## Captured targetability

Components, features, and relationships require a `targetability` object with
all five boolean fields: `suppress`, `unsuppress`, `hide`, `unhide`, and
`delete`. These are captured capabilities, not inferred policies based on
`cadType`. Capturing this object on an entity does not by itself make that
entity kind a supported DSL action target.

Engine semantic validation consumes captured capability information for accepted
target actions. Full target resolution, capability gating, mutation mapping, and
handoff rules belong to the
[target-action contract](../reference/target-action-contract.md).

## Product structure

`structure.rootComponentId` must equal `rootProduct.id` and resolve to an
assembly. Every component needs exactly one node entry. Each node requires
`componentId`, `parentComponentId`, and a `children` array, which may be empty.

Exactly one node has an empty parent ID, and it must identify the root.
Nonempty parent IDs and child IDs must resolve to components. Duplicate nodes,
duplicate children, self-parent/self-child references, conflicting declared
parents, and children without node entries are rejected. The validator checks
cycles reachable from the declared root through child links.

## Validation and canonical JSON

`cad.Parse`, `Read`, and `Load` enforce JSON shape and semantic validation.
Errors distinguish I/O, decoding, and contract validation. Unknown fields,
missing required fields, invalid scalar types, duplicate IDs, and dangling
references are rejected. The validator reports problems rather than repairing
the capture.

`cad.CanonicalJSON` validates and copies the contract, sorts all six entity
registries by ID, sorts structure nodes by component ID, and sorts each child
list lexicographically. It preserves relationship endpoint order and does not
modify the input contract. Repeated equivalent serialization is covered by
canonicalization tests.

For project ingestion and model resource resolution, see
[project mapping](project-mapping.md). Authoring-time value semantics belong to
[DSL semantics](dsl-semantics.md); plans belong to
[IR and planning](ir-and-planning.md).
