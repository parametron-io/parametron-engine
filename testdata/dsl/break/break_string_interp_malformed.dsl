dsl v1.0
// String value contains a malformed interpolation placeholder: {param:} has an empty name.
// Expected: parse error — "malformed interpolation placeholder: empty parameter name"
product Widget {
    param label: string = "prefix-{param:}-suffix"
}
