dsl v1.0
profile LayerTest {
    output_dir = "layer_test_v2"
    file_pattern = "{product}_v2"
}

use profile LayerTest

product Bracket {
    param width: number = 100
    param height: number = 50
}