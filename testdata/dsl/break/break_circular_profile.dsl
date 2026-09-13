dsl v1.0
profile A {
    output_dir = "{profile:B}/nested"
}

profile B {
    output_dir = "{profile:A}/other"
}

use profile A

product X { param x = 1 }