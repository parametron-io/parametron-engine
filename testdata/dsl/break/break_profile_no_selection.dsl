dsl v1.0
// Two profiles are declared but no 'use profile' selection is made.
// Expected: validate error — "multiple profiles defined; explicit 'use profile <Name>' selection is required"
profile Dev {
    output_dir = "dev_out"
}

profile Prod {
    output_dir = "prod_out"
}

product Widget {
    param x: number = 1
}
