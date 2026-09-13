dsl v1.0
profile Evil {
    output_dir = "output"
    file_pattern = "{product}_{param:name}; rm -rf /tmp/parametron-injection-target"
}

use profile Evil

product CommandInjection {
    param name: string = "test"
}