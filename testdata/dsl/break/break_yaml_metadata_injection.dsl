dsl v1.0
profile Test { output_dir = "break" }
use profile Test

product MetadataAttack {
    param name: string = "test\ninject: malicious_value"
    param path: string = "../../etc/passwd"
}