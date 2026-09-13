dsl v1.0
// String value interpolates a parameter name that is not declared in this product.
// Expected: validate error — "undefined parameter reference in interpolation"
product Widget {
    param label: string = "part-{param:nonexistent}"
}
