package table

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const SchemaVersion = "1.0"

type ColumnType string

const (
	ColumnTypeString  ColumnType = "string"
	ColumnTypeNumber  ColumnType = "number"
	ColumnTypeBoolean ColumnType = "boolean"
)

var (
	ErrIO         = errors.New("table I/O error")
	ErrDecode     = errors.New("table decode error")
	ErrValidation = errors.New("table validation error")
	ErrRuntime    = errors.New("table runtime error")
)

type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string {
	if e == nil {
		return ErrIO.Error()
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *FileError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrIO}
	}
	return []error{ErrIO, e.Err}
}

type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string {
	if e == nil || e.Err == nil {
		return ErrDecode.Error()
	}
	return fmt.Sprintf("%s: %v", ErrDecode, e.Err)
}

func (e *DecodeError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrDecode}
	}
	return []error{ErrDecode, e.Err}
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil || e.Message == "" {
		return ErrValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrValidation, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

type RuntimeError struct {
	TableName string
	Operation string
	KeyColumn string
	KeyValue  string
	Column    string
	Message   string
	Err       error
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return ErrRuntime.Error()
	}

	parts := make([]string, 0, 5)
	if e.TableName != "" {
		parts = append(parts, fmt.Sprintf("table %q", e.TableName))
	}
	if e.Operation != "" {
		parts = append(parts, e.Operation)
	}
	if e.KeyColumn != "" || e.KeyValue != "" {
		parts = append(parts, fmt.Sprintf("%s=%q", e.KeyColumn, e.KeyValue))
	}
	if e.Column != "" {
		parts = append(parts, fmt.Sprintf("column %q", e.Column))
	}

	detail := e.Message
	if detail == "" && e.Err != nil {
		detail = e.Err.Error()
	}
	if detail != "" {
		parts = append(parts, detail)
	}
	if len(parts) == 0 {
		return ErrRuntime.Error()
	}

	return fmt.Sprintf("%s: %s", ErrRuntime, strings.Join(parts, ": "))
}

func (e *RuntimeError) Unwrap() []error {
	if e == nil || e.Err == nil {
		return []error{ErrRuntime}
	}
	return []error{ErrRuntime, e.Err}
}

type Table struct {
	SchemaVersion string
	Name          string
	KeyColumn     string
	Columns       []Column
	Rows          []Row
}

type Column struct {
	Name     string
	Type     ColumnType
	Required bool
}

type Row struct {
	Values map[string]Value
}

type Value struct {
	Type    ColumnType
	String  string
	Number  json.Number
	Boolean bool
}

func Parse(data []byte) (*Table, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, &DecodeError{Err: err}
	}

	if err := ensureNoTrailingJSON(decoder); err != nil {
		return nil, &DecodeError{Err: err}
	}

	root, ok := raw.(map[string]any)
	if !ok {
		return nil, &ValidationError{Message: "root must be a JSON object"}
	}

	table, err := parseTableObject(root)
	if err != nil {
		return nil, err
	}

	return table, nil
}

func LoadFile(path string) (*Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{Path: path, Err: err}
	}
	return Parse(data)
}

func Validate(table *Table) error {
	return validateTable(table)
}

func CanonicalJSON(table *Table) ([]byte, error) {
	if err := Validate(table); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.WriteByte('{')
	writeJSONString(&out, "schemaVersion")
	out.WriteByte(':')
	writeJSONString(&out, table.SchemaVersion)
	out.WriteByte(',')
	writeJSONString(&out, "name")
	out.WriteByte(':')
	writeJSONString(&out, table.Name)
	out.WriteByte(',')
	writeJSONString(&out, "keyColumn")
	out.WriteByte(':')
	writeJSONString(&out, table.KeyColumn)
	out.WriteByte(',')
	writeJSONString(&out, "columns")
	out.WriteByte(':')
	out.WriteByte('[')
	for i, column := range table.Columns {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		writeJSONString(&out, "name")
		out.WriteByte(':')
		writeJSONString(&out, column.Name)
		out.WriteByte(',')
		writeJSONString(&out, "type")
		out.WriteByte(':')
		writeJSONString(&out, string(column.Type))
		out.WriteByte(',')
		writeJSONString(&out, "required")
		out.WriteByte(':')
		if column.Required {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
	out.WriteByte(',')
	writeJSONString(&out, "rows")
	out.WriteByte(':')
	out.WriteByte('[')
	for rowIndex, row := range table.Rows {
		if rowIndex > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		wroteField := false
		for _, column := range table.Columns {
			value, ok := row.Values[column.Name]
			if !ok {
				continue
			}
			if wroteField {
				out.WriteByte(',')
			}
			writeJSONString(&out, column.Name)
			out.WriteByte(':')
			if err := writeCanonicalValue(&out, value); err != nil {
				return nil, err
			}
			wroteField = true
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
	out.WriteByte('}')

	return out.Bytes(), nil
}

func ComputeFingerprint(table *Table) (string, error) {
	data, err := CanonicalJSON(table)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (t *Table) Fingerprint() (string, error) {
	return ComputeFingerprint(t)
}

func (t *Table) RowByKey(key string) (Row, error) {
	if err := Validate(t); err != nil {
		return Row{}, err
	}

	for _, row := range t.Rows {
		if row.Values[t.KeyColumn].String == key {
			return cloneRow(row), nil
		}
	}

	return Row{}, &RuntimeError{
		TableName: t.Name,
		Operation: "row lookup",
		KeyColumn: t.KeyColumn,
		KeyValue:  key,
		Message:   "row not found",
	}
}

func cloneRow(row Row) Row {
	if row.Values == nil {
		return Row{}
	}

	values := make(map[string]Value, len(row.Values))
	for key, value := range row.Values {
		values[key] = value
	}

	return Row{Values: values}
}

func parseTableObject(root map[string]any) (*Table, error) {
	if err := ensureAllowedKeys(root, "root", "schemaVersion", "name", "keyColumn", "columns", "rows"); err != nil {
		return nil, err
	}

	schemaVersion, err := requiredStringField(root, "schemaVersion")
	if err != nil {
		return nil, err
	}
	if schemaVersion != SchemaVersion {
		return nil, &ValidationError{Message: fmt.Sprintf("schemaVersion must be %q", SchemaVersion)}
	}

	name, err := requiredStringField(root, "name")
	if err != nil {
		return nil, err
	}

	keyColumn, err := requiredStringField(root, "keyColumn")
	if err != nil {
		return nil, err
	}

	columnsValue, ok := root["columns"]
	if !ok {
		return nil, &ValidationError{Message: "columns is required"}
	}
	columnsRaw, ok := columnsValue.([]any)
	if !ok {
		return nil, &ValidationError{Message: "columns must be an array"}
	}
	if len(columnsRaw) == 0 {
		return nil, &ValidationError{Message: "columns must be non-empty"}
	}

	columns := make([]Column, 0, len(columnsRaw))
	columnByName := make(map[string]Column, len(columnsRaw))
	for index, entry := range columnsRaw {
		columnObject, ok := entry.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("column %d must be an object", index)}
		}
		if err := ensureAllowedKeys(columnObject, fmt.Sprintf("column %d", index), "name", "type", "required"); err != nil {
			return nil, err
		}

		columnName, err := requiredStringField(columnObject, "name")
		if err != nil {
			return nil, &ValidationError{Message: fmt.Sprintf("column %d: %s", index, validationMessage(err))}
		}
		if _, exists := columnByName[columnName]; exists {
			return nil, &ValidationError{Message: fmt.Sprintf("column names must be unique: %q", columnName)}
		}

		columnTypeText, err := requiredStringField(columnObject, "type")
		if err != nil {
			return nil, &ValidationError{Message: fmt.Sprintf("column %d: %s", index, validationMessage(err))}
		}
		columnType := ColumnType(columnTypeText)
		if !isSupportedColumnType(columnType) {
			return nil, &ValidationError{Message: fmt.Sprintf("column %q has unsupported type %q", columnName, columnTypeText)}
		}

		requiredValue, ok := columnObject["required"]
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("column %q: required is required", columnName)}
		}
		required, ok := requiredValue.(bool)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("column %q: required must be a boolean", columnName)}
		}

		column := Column{
			Name:     columnName,
			Type:     columnType,
			Required: required,
		}
		columns = append(columns, column)
		columnByName[columnName] = column
	}

	keyColumnDef, ok := columnByName[keyColumn]
	if !ok {
		return nil, &ValidationError{Message: fmt.Sprintf("keyColumn %q must exist in columns", keyColumn)}
	}
	if keyColumnDef.Type != ColumnTypeString {
		return nil, &ValidationError{Message: fmt.Sprintf("keyColumn %q must have type %q", keyColumn, ColumnTypeString)}
	}
	if !keyColumnDef.Required {
		return nil, &ValidationError{Message: fmt.Sprintf("keyColumn %q must be required", keyColumn)}
	}

	rowsValue, ok := root["rows"]
	if !ok {
		return nil, &ValidationError{Message: "rows is required"}
	}
	rowsRaw, ok := rowsValue.([]any)
	if !ok {
		return nil, &ValidationError{Message: "rows must be an array"}
	}

	rows := make([]Row, 0, len(rowsRaw))
	seenKeys := make(map[string]struct{}, len(rowsRaw))

	for rowIndex, entry := range rowsRaw {
		rowObject, ok := entry.(map[string]any)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("row %d must be an object", rowIndex)}
		}

		if err := validateRowKeys(rowObject, columns, rowIndex); err != nil {
			return nil, err
		}

		row := Row{Values: make(map[string]Value, len(rowObject))}
		for _, column := range columns {
			rawValue, present := rowObject[column.Name]
			if !present {
				if column.Required {
					return nil, &ValidationError{Message: fmt.Sprintf("row %d is missing required column %q", rowIndex, column.Name)}
				}
				continue
			}

			value, err := parseValue(rawValue, column, rowIndex)
			if err != nil {
				return nil, err
			}
			row.Values[column.Name] = value
		}

		keyValue := row.Values[keyColumn].String
		if strings.TrimSpace(keyValue) == "" {
			return nil, &ValidationError{Message: fmt.Sprintf("row %d keyColumn %q must be a non-empty string", rowIndex, keyColumn)}
		}
		if _, exists := seenKeys[keyValue]; exists {
			return nil, &ValidationError{Message: fmt.Sprintf("duplicate keyColumn value %q", keyValue)}
		}
		seenKeys[keyValue] = struct{}{}
		rows = append(rows, row)
	}

	table := &Table{
		SchemaVersion: schemaVersion,
		Name:          name,
		KeyColumn:     keyColumn,
		Columns:       columns,
		Rows:          rows,
	}

	if err := Validate(table); err != nil {
		return nil, err
	}

	return table, nil
}

func validateTable(table *Table) error {
	if table == nil {
		return &ValidationError{Message: "table must not be nil"}
	}
	if table.SchemaVersion != SchemaVersion {
		return &ValidationError{Message: fmt.Sprintf("schemaVersion must be %q", SchemaVersion)}
	}
	if strings.TrimSpace(table.Name) == "" {
		return &ValidationError{Message: "name is required"}
	}
	if strings.TrimSpace(table.KeyColumn) == "" {
		return &ValidationError{Message: "keyColumn is required"}
	}
	if len(table.Columns) == 0 {
		return &ValidationError{Message: "columns must be non-empty"}
	}
	if table.Rows == nil {
		return &ValidationError{Message: "rows is required"}
	}

	columnByName := make(map[string]Column, len(table.Columns))
	for index, column := range table.Columns {
		if strings.TrimSpace(column.Name) == "" {
			return &ValidationError{Message: fmt.Sprintf("column %d: name is required", index)}
		}
		if !isSupportedColumnType(column.Type) {
			return &ValidationError{Message: fmt.Sprintf("column %q has unsupported type %q", column.Name, column.Type)}
		}
		if _, exists := columnByName[column.Name]; exists {
			return &ValidationError{Message: fmt.Sprintf("column names must be unique: %q", column.Name)}
		}
		columnByName[column.Name] = column
	}

	keyColumn, ok := columnByName[table.KeyColumn]
	if !ok {
		return &ValidationError{Message: fmt.Sprintf("keyColumn %q must exist in columns", table.KeyColumn)}
	}
	if keyColumn.Type != ColumnTypeString {
		return &ValidationError{Message: fmt.Sprintf("keyColumn %q must have type %q", table.KeyColumn, ColumnTypeString)}
	}
	if !keyColumn.Required {
		return &ValidationError{Message: fmt.Sprintf("keyColumn %q must be required", table.KeyColumn)}
	}

	seenKeys := make(map[string]struct{}, len(table.Rows))
	for rowIndex, row := range table.Rows {
		rowValues := row.Values
		if rowValues == nil {
			rowValues = map[string]Value{}
		}

		unknownColumns := make([]string, 0)
		for columnName := range rowValues {
			if _, ok := columnByName[columnName]; !ok {
				unknownColumns = append(unknownColumns, columnName)
			}
		}
		sort.Strings(unknownColumns)
		if len(unknownColumns) > 0 {
			return &ValidationError{Message: fmt.Sprintf("row %d has unknown column %q", rowIndex, unknownColumns[0])}
		}

		for _, column := range table.Columns {
			value, present := rowValues[column.Name]
			if !present {
				if column.Required {
					return &ValidationError{Message: fmt.Sprintf("row %d is missing required column %q", rowIndex, column.Name)}
				}
				continue
			}

			if err := validateTypedValue(value, column, rowIndex); err != nil {
				return err
			}
		}

		keyValue := rowValues[table.KeyColumn].String
		if strings.TrimSpace(keyValue) == "" {
			return &ValidationError{Message: fmt.Sprintf("row %d keyColumn %q must be a non-empty string", rowIndex, table.KeyColumn)}
		}
		if _, exists := seenKeys[keyValue]; exists {
			return &ValidationError{Message: fmt.Sprintf("duplicate keyColumn value %q", keyValue)}
		}
		seenKeys[keyValue] = struct{}{}
	}

	return nil
}

func validateRowKeys(rowObject map[string]any, columns []Column, rowIndex int) error {
	allowed := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		allowed[column.Name] = struct{}{}
	}

	extraFields := make([]string, 0)
	for name := range rowObject {
		if _, ok := allowed[name]; !ok {
			extraFields = append(extraFields, name)
		}
	}
	sort.Strings(extraFields)
	if len(extraFields) > 0 {
		return &ValidationError{Message: fmt.Sprintf("row %d has unknown column %q", rowIndex, extraFields[0])}
	}

	return nil
}

func parseValue(rawValue any, column Column, rowIndex int) (Value, error) {
	if rawValue == nil {
		return Value{}, &ValidationError{Message: fmt.Sprintf("row %d column %q must not be null", rowIndex, column.Name)}
	}

	switch column.Type {
	case ColumnTypeString:
		text, ok := rawValue.(string)
		if !ok {
			return Value{}, &ValidationError{Message: fmt.Sprintf("row %d column %q must be a string", rowIndex, column.Name)}
		}
		return Value{Type: ColumnTypeString, String: text}, nil
	case ColumnTypeNumber:
		number, ok := rawValue.(json.Number)
		if !ok {
			return Value{}, &ValidationError{Message: fmt.Sprintf("row %d column %q must be a number", rowIndex, column.Name)}
		}
		if !isValidJSONNumber(number) {
			return Value{}, &ValidationError{Message: fmt.Sprintf("row %d column %q must be a valid number", rowIndex, column.Name)}
		}
		return Value{Type: ColumnTypeNumber, Number: number}, nil
	case ColumnTypeBoolean:
		boolean, ok := rawValue.(bool)
		if !ok {
			return Value{}, &ValidationError{Message: fmt.Sprintf("row %d column %q must be a boolean", rowIndex, column.Name)}
		}
		return Value{Type: ColumnTypeBoolean, Boolean: boolean}, nil
	default:
		return Value{}, &ValidationError{Message: fmt.Sprintf("row %d column %q has unsupported type %q", rowIndex, column.Name, column.Type)}
	}
}

func validateTypedValue(value Value, column Column, rowIndex int) error {
	if value.Type != column.Type {
		return &ValidationError{Message: fmt.Sprintf("row %d column %q must have type %q", rowIndex, column.Name, column.Type)}
	}

	switch column.Type {
	case ColumnTypeString:
		return nil
	case ColumnTypeNumber:
		if value.Number == "" {
			return &ValidationError{Message: fmt.Sprintf("row %d column %q must be a number", rowIndex, column.Name)}
		}
		if !isValidJSONNumber(value.Number) {
			return &ValidationError{Message: fmt.Sprintf("row %d column %q must be a valid number", rowIndex, column.Name)}
		}
		return nil
	case ColumnTypeBoolean:
		return nil
	default:
		return &ValidationError{Message: fmt.Sprintf("row %d column %q has unsupported type %q", rowIndex, column.Name, column.Type)}
	}
}

func writeCanonicalValue(out *bytes.Buffer, value Value) error {
	switch value.Type {
	case ColumnTypeString:
		writeJSONString(out, value.String)
		return nil
	case ColumnTypeNumber:
		if value.Number == "" {
			return &ValidationError{Message: "number value must not be empty"}
		}
		out.WriteString(value.Number.String())
		return nil
	case ColumnTypeBoolean:
		if value.Boolean {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return nil
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported value type %q", value.Type)}
	}
}

func writeJSONString(out *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	out.Write(encoded)
}

func requiredStringField(object map[string]any, field string) (string, error) {
	value, ok := object[field]
	if !ok {
		return "", &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	text, ok := value.(string)
	if !ok {
		return "", &ValidationError{Message: fmt.Sprintf("%s must be a string", field)}
	}
	if strings.TrimSpace(text) == "" {
		return "", &ValidationError{Message: fmt.Sprintf("%s is required", field)}
	}
	return text, nil
}

func ensureAllowedKeys(object map[string]any, context string, allowedKeys ...string) error {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}

	extraKeys := make([]string, 0)
	for key := range object {
		if _, ok := allowed[key]; !ok {
			extraKeys = append(extraKeys, key)
		}
	}
	sort.Strings(extraKeys)
	if len(extraKeys) > 0 {
		return &ValidationError{Message: fmt.Sprintf("%s has unknown field %q", context, extraKeys[0])}
	}

	return nil
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("unexpected trailing JSON content")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func isSupportedColumnType(columnType ColumnType) bool {
	switch columnType {
	case ColumnTypeString, ColumnTypeNumber, ColumnTypeBoolean:
		return true
	default:
		return false
	}
}

func isValidJSONNumber(number json.Number) bool {
	decoder := json.NewDecoder(strings.NewReader(number.String()))
	decoder.UseNumber()

	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return false
	}
	if err := ensureNoTrailingJSON(decoder); err != nil {
		return false
	}

	_, ok := raw.(json.Number)
	return ok
}

func validationMessage(err error) string {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) && validationErr.Message != "" {
		return validationErr.Message
	}
	return err.Error()
}
