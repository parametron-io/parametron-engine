dsl v1.0

const ORG = "ACME"

profile EscapeSmoke {
    output_dir = "escape_smoke_out"
}

use profile EscapeSmoke

product EscapeLabel {
    param org: string = ORG
    param series: string = "X1"

    // concat: build full_id from two string params
    param full_id: string = org + "-" + series

    // interpolation: embed a string param in a literal
    param display: string = "ID: {param:full_id}"

    // escaped braces: \{ and \} remain literal, no interpolation
    param literal_tmpl: string = "template: \{param:series\}"

    // escaped backslash: \\ produces a single backslash in the value
    param path_seg: string = "data\\files"

    // escaped quote: \" embeds a double-quote inside the string
    param quoted_note: string = "status: \"active\""
}
