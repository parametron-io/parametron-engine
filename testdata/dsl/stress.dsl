dsl v1.0
const PROJECT = "ParametronStress"
const DEFAULT_MATERIAL = "steel"
const ISO_COUNTRY = "TR"
const PI = 3.1415926
const BASE_THK = 4
const HOLE_D = 8
const EDGE_CLEAR = 12

profile Dev {
  output_dir = "stress_dev"
  file_pattern = "{const:PROJECT}_{product}_{profile}_{param:width}_{plan_hash}"
}

profile Prod {
  output_dir = "stress_prod"
}

use profile Dev

product Bracket {
  param name: string = PROJECT
  param material: string = DEFAULT_MATERIAL
  param country: string = ISO_COUNTRY

  param width: number = 120
  param height: number = 80
  param thickness: number = BASE_THK
  param corner_r: number = 6

  param finish: enum { Raw, Zinc, Paint, Anodize } = Paint
  param finish_is_paint: boolean = finish == Paint
  param finish_is_raw: boolean = finish == Raw

  param is_steel: boolean = material == "steel"
  param is_aluminum: boolean = material == "aluminum"
  param is_tr: boolean = country == "TR"
  param is_eu: boolean = country != "TR"

  param area: number = width * height
  param perimeter: number = 2 * (width + height)

  param density: number = is_steel ? 7850 : (is_aluminum ? 2700 : 5000)

  param volume_mm3: number = area * thickness
  param volume_m3: number = volume_mm3 / 1000000000
  param mass_kg: number = volume_m3 * density

  param hole_d: number = HOLE_D
  param hole_edge_clear: number = EDGE_CLEAR

  param min_dim_ok: boolean = (width > (2 * hole_edge_clear + hole_d)) && (height > (2 * hole_edge_clear + hole_d))

  param hole_count_x: number = min_dim_ok ? round(max(1, (width - 2 * hole_edge_clear) / 40)) : 0
  param hole_count_y: number = min_dim_ok ? round(max(1, (height - 2 * hole_edge_clear) / 40)) : 0
  param hole_count_total: number = hole_count_x * hole_count_y

  param base_cost: number = 3.5
  param material_mult: number = is_steel ? 1.0 : (is_aluminum ? 1.6 : 1.3)
  param finish_mult: number =
    finish == Raw ? 1.0 :
    finish == Zinc ? 1.15 :
    finish == Paint ? 1.35 :
    1.40

  param hole_cost: number = hole_count_total * 0.08
  param size_cost: number = (area / 10000) * 0.12

  param warp_risk: number = abs((width - height) / max(1, min(width, height)))
  param risk_mult: number = warp_risk > 0.25 ? 1.08 : 1.00

  param unit_price: number = (base_cost + hole_cost + size_cost) * material_mult * finish_mult * risk_mult

  param tag_export: string = finish_is_raw ? "EXPORT_RAW" : (finish_is_paint ? "EXPORT_PAINT" : "EXPORT_OTHER")
  param requires_qc: boolean = (unit_price > 8.0) || (hole_count_total > 12)
  param qc_level: enum { Low, Medium, High } = requires_qc ? High : Medium

  param diag: number = round((width*width + height*height) / max(1, (width + height)))
  param corner_area: number = PI * corner_r * corner_r
  param net_area: number = area - min(area * 0.15, corner_area)

  param shipping_class: enum { XS, S, M, L, XL } =
    mass_kg < 0.2 ? XS :
    mass_kg < 0.6 ? S :
    mass_kg < 1.5 ? M :
    mass_kg < 4.0 ? L :
    XL

  param premium: boolean = is_eu && (finish != Raw) && (unit_price > 9.0)
  param discount_ok: boolean = !premium && (hole_count_total < 8)

  param score: number =
    (premium ? 10 : 6) +
    (requires_qc ? 2 : 0) +
    (finish == Paint ? 1 : 0) +
    round(min(5, hole_count_total / 3))

  param note_1: string = premium ? "PREMIUM" : "STANDARD"
  param note_2: string = discount_ok ? "DISCOUNT_OK" : "NO_DISCOUNT"
  param note_3: string = is_tr ? "LOCAL" : "EXPORT"

  // String expressions v1 coverage
  param label: string = note_1 + "-" + note_3
  param display: string = "Bracket: {param:label}"
  param literal_ref: string = "ref: \{param:note_1\}"
  param export_path: string = "out\\" + tag_export

  // Multi-placeholder interpolation
  param display_full: string = "{param:note_1} | {param:note_2} | {param:note_3}"

  // Interpolation mixed with concatenation
  param qc_annotation: string = note_1 + ": {param:note_2}"

  // Empty string as identity element for concat
  param safe_label: string = "" + note_1

  // Unary NOT coverage: !a, !!a, !(a && b), a || !b
  param not_requires_qc: boolean = !requires_qc
  param confirmed_qc: boolean = !!requires_qc
  param not_both_paint_and_tr: boolean = !(finish_is_paint && is_tr)
  param local_or_not_premium: boolean = is_tr || !premium

  // Boolean == and != comparisons
  param qc_eq_premium: boolean = requires_qc == premium
  param qc_ne_premium: boolean = requires_qc != premium

  // Number == and != on computed values
  param score_at_base: boolean = score == 8
  param price_above_base: boolean = unit_price != base_cost

  // String == and != on computed values
  param notes_same: boolean = note_1 == note_2
  param not_local_export: boolean = note_3 != "EXPORT"

  // Enum == and != on derived enum values
  param is_medium_qc: boolean = qc_level == Medium
  param not_xl_ship: boolean = shipping_class != XL

  // Equality nested inside ternary
  param cost_tier: string = (unit_price != base_cost) ? "ADJUSTED" : "BASE"
  param ship_tier: string = (shipping_class == S) ? "LIGHT" : "HEAVY"

  // Numeric expansion: floor, ceil, clamp, round_up, round_down
  param thk_floor: number = floor(thickness * 1.7)
  param thk_ceil: number = ceil(thickness * 1.3)
  param clamped_score: number = clamp(score, 0, 15)
  param cost_up: number = round_up(unit_price, 0.5)
  param area_down: number = round_down(area, 1000)
}

product Panel {
  param panel_name: string = PROJECT
  param length: number = 900
  param width: number = 450
  param thk: number = max(2, BASE_THK - 1)

  param coating: enum { None, Powder, Galvanized } = Powder
  param coating_cost: number = coating == None ? 0.0 : (coating == Powder ? 2.2 : 1.5)

  param cutouts: number = 6
  param cutout_ok: boolean = (cutouts > 0) && (length > 200) && (width > 200)

  param cutout_area: number = cutout_ok ? (cutouts * 1200) : 0
  param gross_area: number = length * width
  param net_area: number = gross_area - cutout_area

  param stiffness_index: number = (thk * thk) / max(1, (length/100))
  param needs_rib: boolean = stiffness_index < 0.6

  param rib_count: number = needs_rib ? round(max(2, length / 250)) : 0
  param rib_cost: number = rib_count * 0.9

  param base_cost: number = 4.0
  param size_cost: number = (net_area / 100000) * 1.1

  param unit_price: number = base_cost + size_cost + coating_cost + rib_cost

  param quality: enum { Low, Normal, High } =
    (unit_price > 12) || needs_rib ? High :
    (unit_price > 8) ? Normal :
    Low

  param tag_a: string = needs_rib ? "RIBBED" : "PLAIN"
  param tag_b: string = coating == Powder ? "COATED" : "UNCOATED"
  param tag_c: string = cutout_ok ? "CUTOUTS_OK" : "CUTOUTS_NONE"
  param tag_d: string = quality == High ? "QC_HIGH" : "QC_STD"

  // String coverage: concat and multi-placeholder interpolation
  param tag_combined: string = tag_a + "_" + tag_b
  param panel_display: string = "Panel-{param:tag_combined} ({param:tag_d})"

  // Unary NOT coverage: !a and !(boolean expr)
  param plain_surface: boolean = !needs_rib
  param plain_or_not_powder: boolean = !needs_rib || !(coating == Powder)

  // Enum equality and inequality on derived values
  param quality_is_normal: boolean = quality == Normal
  param has_coating: boolean = coating != None

  // Numeric expansion in Panel: rounding direction is observable in every case
  param section_count: number = floor(length / 7)
  param cutout_cells: number = ceil(net_area / 10000)
  param stiff_clamped: number = clamp(stiffness_index, 0, 5)
  param price_up: number = round_up(unit_price, 1)
  param area_snapped: number = round_down(gross_area, 10000)

  // Number == and != on Panel dimensions
  param same_dims: boolean = length == width
  param diff_dims: boolean = length != width

  // Boolean == and != between derived flags
  param rib_eq_cutout: boolean = needs_rib == cutout_ok
  param rib_ne_cutout: boolean = needs_rib != cutout_ok

  // Unary NOT: !!a, !(a && b), a && !b
  param confirmed_plain: boolean = !!plain_surface
  param not_rib_and_coating: boolean = !(needs_rib && has_coating)
  param ok_and_not_rib: boolean = cutout_ok && !needs_rib

  // String: concat + interpolation combined, and computed string ==
  param panel_full_tag: string = "panel:{param:tag_combined}" + "-" + tag_d
  param tag_eq_plain: boolean = tag_a == "PLAIN"
}
