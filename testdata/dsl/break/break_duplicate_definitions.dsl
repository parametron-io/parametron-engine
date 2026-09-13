dsl v1.0
const DUP = 1
const DUP = 2

profile Test { output_dir = "break" }

product Test {
    param a: number = 1
    param a: number = 2
}

product Test { 
    param b: number = 3
}