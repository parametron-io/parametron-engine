package dsl

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

var reservedIdentifiers = map[string]bool{
	// Statement keywords reserved for future language extensions
	"if": true, "else": true, "for": true, "while": true,
	"return": true, "break": true, "continue": true,

	// Type keywords (cannot be used as variable names)
	"number": true, "string": true, "boolean": true, "enum": true,

	// Literal keywords
	"true": true, "false": true,

	// DSL keywords
	"const": true, "profile": true, "product": true, "param": true, "let": true, "use": true, "dsl": true,
	"target": true, "action": true,
}

var validTypeIdentifiers = map[string]bool{
	"number":  true,
	"string":  true,
	"boolean": true,
	"enum":    true,
}

const SupportedDSLVersion = "1.0"

// Parse reads the DSL file at the given path and returns the parsed AST.
func Parse(filePath string) (*AST, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read DSL file: %w", err)
	}
	sourcePath := filePath
	if absPath, err := filepath.Abs(filePath); err == nil {
		sourcePath = absPath
	}

	p := &Parser{
		input:                   []rune(string(content)),
		line:                    1,
		col:                     1,
		requireVersionDirective: true,
	}
	ast, err := p.parse()
	if err != nil {
		return nil, err
	}
	ast.SourcePath = sourcePath
	return ast, nil
}

type Parser struct {
	input                   []rune
	pos                     int
	line                    int
	col                     int
	lexErr                  error
	requireVersionDirective bool
	disableInterpolation    bool
}

func (p *Parser) parse() (*AST, error) {
	ast := &AST{}
	if p.requireVersionDirective {
		version, err := p.parseVersionDirective()
		if err != nil {
			return nil, err
		}
		ast.DSLVersion = version
	}

	for !p.isEOF() {
		if p.lexErr != nil {
			return nil, p.lexErr
		}

		token := p.peek()
		if token == "" {
			if p.lexErr != nil {
				return nil, p.lexErr
			}
			break
		}

		if token == "const" {
			constDecl, err := p.parseConst()
			if err != nil {
				return nil, err
			}
			ast.Constants = append(ast.Constants, constDecl)
		} else if token == "profile" {
			profileDecl, err := p.parseProfile()
			if err != nil {
				return nil, err
			}
			ast.Profiles = append(ast.Profiles, profileDecl)
		} else if token == "use" {
			if ast.ActiveProfileName != nil {
				return nil, p.errorf("multiple 'use profile' statements are not allowed")
			}
			activeName, err := p.parseUseProfile()
			if err != nil {
				return nil, err
			}
			ast.ActiveProfileName = &activeName
		} else if token == "product" {
			prod, err := p.parseProduct()
			if err != nil {
				return nil, err
			}
			ast.Products = append(ast.Products, prod)
		} else if token == "dsl" {
			return nil, p.errorf("version directive must appear at top of file")
		} else {
			return nil, p.errorf("unexpected token '%s', expected 'const', 'profile', 'use', or 'product'", token)
		}
	}

	if p.lexErr != nil {
		return nil, p.lexErr
	}

	return ast, nil
}

func (p *Parser) parseVersionDirective() (string, error) {
	first := p.peek()
	if first != "dsl" {
		if p.hasTokenAhead("dsl") {
			return "", p.errorf("version directive must appear at top of file")
		}
		return "", p.errorf("missing version directive")
	}

	if err := p.expect("dsl"); err != nil {
		return "", err
	}

	versionToken := p.consume()
	if versionToken == "" {
		return "", p.errorf("missing version directive")
	}

	version := versionToken
	if strings.HasPrefix(version, "v") {
		version = strings.TrimPrefix(version, "v")
	}

	if version != SupportedDSLVersion {
		return "", p.errorf("unsupported DSL version")
	}

	return SupportedDSLVersion, nil
}

func (p *Parser) hasTokenAhead(target string) bool {
	savedPos, savedLine, savedCol, savedLexErr := p.pos, p.line, p.col, p.lexErr
	defer func() {
		p.pos, p.line, p.col, p.lexErr = savedPos, savedLine, savedCol, savedLexErr
	}()

	for !p.isEOF() {
		tok := p.consume()
		if p.lexErr != nil {
			return false
		}
		if tok == target {
			return true
		}
	}

	return false
}

func (p *Parser) parseConst() (*ConstDeclarationNode, error) {
	if err := p.expect("const"); err != nil {
		return nil, err
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.expect("="); err != nil {
		return nil, err
	}

	expr, err := p.parseExpression(0)
	if err != nil {
		return nil, err
	}

	return &ConstDeclarationNode{
		Name:  name,
		Value: expr,
	}, nil
}

func (p *Parser) parseProduct() (*ProductNode, error) {
	if err := p.expect("product"); err != nil {
		return nil, err
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.expect("{"); err != nil {
		return nil, err
	}

	prod := &ProductNode{
		Name:       name,
		Parameters: []*ParameterNode{},
	}
	seenTargetActions := make(map[string]struct{})

	for !p.isEOF() {
		if p.lexErr != nil {
			return nil, p.lexErr
		}

		token := p.peek()
		if token == "}" {
			p.consume()
			return prod, nil
		}

		if token == "let" {
			letDecl, err := p.parseLet()
			if err != nil {
				return nil, err
			}
			prod.Lets = append(prod.Lets, letDecl)
		} else if token == "param" {
			param, err := p.parseParameter()
			if err != nil {
				return nil, err
			}
			prod.Parameters = append(prod.Parameters, param)
		} else if token == "target" {
			targetAction, err := p.parseTargetAction()
			if err != nil {
				return nil, err
			}
			if _, exists := seenTargetActions[targetAction.SemanticTarget]; exists {
				return nil, p.errorf("duplicate target action declaration for semantic target '%s' in product '%s'", targetAction.SemanticTarget, name)
			}
			seenTargetActions[targetAction.SemanticTarget] = struct{}{}
			prod.TargetActions = append(prod.TargetActions, targetAction)
		} else if token == "adapter" || token == "source_model" || token == "outputs" {
			key := p.consume()
			if err := p.expect("="); err != nil {
				return nil, err
			}
			prevDisable := p.disableInterpolation
			p.disableInterpolation = true
			value, err := p.parseExpression(0)
			p.disableInterpolation = prevDisable
			if err != nil {
				return nil, err
			}

			switch key {
			case "adapter":
				if prod.Adapter != nil {
					return nil, p.errorf("duplicate product execution declaration key '%s' in product '%s'", key, name)
				}
				prod.Adapter = value
			case "source_model":
				if prod.SourceModel != nil {
					return nil, p.errorf("duplicate product execution declaration key '%s' in product '%s'", key, name)
				}
				prod.SourceModel = value
			case "outputs":
				if prod.Outputs != nil {
					return nil, p.errorf("duplicate product execution declaration key '%s' in product '%s'", key, name)
				}
				prod.Outputs = value
			}
			prod.ExecutionDeclarationOrder = append(prod.ExecutionDeclarationOrder, key)
		} else {
			return nil, p.errorf("unexpected token '%s' inside product '%s', expected 'adapter', 'source_model', 'outputs', 'let', 'param', 'target', or '}'", token, name)
		}
	}

	if p.lexErr != nil {
		return nil, p.lexErr
	}

	return nil, p.errorf("unexpected EOF inside product '%s'", name)
}

func (p *Parser) parseTargetAction() (*TargetActionNode, error) {
	if err := p.expect("target"); err != nil {
		return nil, err
	}

	semanticTarget, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.expect(":"); err != nil {
		return nil, err
	}
	if err := p.expect("action"); err != nil {
		return nil, err
	}
	if err := p.expect("="); err != nil {
		return nil, err
	}

	action, err := p.parseExpression(0)
	if err != nil {
		return nil, err
	}

	return &TargetActionNode{
		SemanticTarget: semanticTarget,
		Action:         action,
	}, nil
}

func (p *Parser) parseLet() (*LetNode, error) {
	if err := p.expect("let"); err != nil {
		return nil, err
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.expect("="); err != nil {
		return nil, err
	}

	expr, err := p.parseExpression(0)
	if err != nil {
		return nil, err
	}

	return &LetNode{
		Name:  name,
		Value: expr,
	}, nil
}

func (p *Parser) parseProfile() (*ProfileDeclarationNode, error) {
	if err := p.expect("profile"); err != nil {
		return nil, err
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.expect("{"); err != nil {
		return nil, err
	}

	profile := &ProfileDeclarationNode{
		Name:         name,
		Settings:     map[string]ExpressionNode{},
		SettingOrder: []string{},
	}

	for !p.isEOF() {
		if p.lexErr != nil {
			return nil, p.lexErr
		}

		token := p.peek()
		if token == "}" {
			p.consume()
			return profile, nil
		}

		key, err := p.parseIdentifier()
		if err != nil {
			return nil, err
		}
		if err := p.expect("="); err != nil {
			return nil, err
		}
		prevDisable := p.disableInterpolation
		p.disableInterpolation = true
		value, err := p.parseExpression(0)
		p.disableInterpolation = prevDisable
		if err != nil {
			return nil, err
		}

		profile.SettingOrder = append(profile.SettingOrder, key)
		profile.Settings[key] = value
	}

	if p.lexErr != nil {
		return nil, p.lexErr
	}

	return nil, p.errorf("unexpected EOF inside profile '%s'", name)
}

func (p *Parser) parseUseProfile() (string, error) {
	if err := p.expect("use"); err != nil {
		return "", err
	}
	if err := p.expect("profile"); err != nil {
		return "", err
	}
	return p.parseIdentifier()
}

func (p *Parser) parseParameter() (*ParameterNode, error) {
	// Grammar: param <identifier>: <type> = <literal>
	if err := p.expect("param"); err != nil {
		return nil, err
	}

	name, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}

	if err := p.expect(":"); err != nil {
		return nil, err
	}

	typeToken, err := p.parseTypeIdentifier()
	if err != nil {
		return nil, err
	}

	var paramType ParameterType
	switch typeToken {
	case "number":
		paramType = ParameterType{Kind: ParamTypeNumber}
	case "string":
		paramType = ParameterType{Kind: ParamTypeString}
	case "boolean":
		paramType = ParameterType{Kind: ParamTypeBoolean}
	case "enum":
		if err := p.expect("{"); err != nil {
			return nil, err
		}

		if p.peek() == "}" {
			return nil, p.errorf("enum type must contain at least one identifier")
		}

		enumValues := []string{}
		for {
			enumValue, err := p.parseIdentifier()
			if err != nil {
				return nil, err
			}
			enumValues = append(enumValues, enumValue)

			next := p.peek()
			if next == "," {
				p.consume()
				if p.peek() == "}" {
					return nil, p.errorf("expected identifier after ',' in enum type")
				}
				continue
			}
			if next == "}" {
				p.consume()
				break
			}
			return nil, p.errorf("expected ',' or '}' in enum type, got '%s'", next)
		}

		paramType = ParameterType{
			Kind:       ParamTypeEnum,
			EnumValues: enumValues,
		}
	default:
		return nil, p.errorf("unknown type '%s'", typeToken)
	}

	if err := p.expect("="); err != nil {
		return nil, err
	}

	expr, err := p.parseExpression(0)
	if err != nil {
		return nil, err
	}

	return &ParameterNode{
		Name:         name,
		Type:         paramType,
		DefaultValue: expr,
	}, nil
}

func (p *Parser) parseIdentifier() (string, error) {
	if p.lexErr != nil {
		return "", p.lexErr
	}

	token := p.consume()
	if p.lexErr != nil {
		return "", p.lexErr
	}

	if token == "" {
		return "", p.errorf("expected identifier, got EOF")
	}
	if !unicode.IsLetter(rune(token[0])) {
		return "", p.errorf("invalid identifier '%s'", token)
	}
	if reservedIdentifiers[token] {
		return "", p.errorf("'%s' is a reserved keyword and cannot be used as an identifier", token)
	}
	return token, nil
}

func (p *Parser) parseTypeIdentifier() (string, error) {
	if p.lexErr != nil {
		return "", p.lexErr
	}

	token := p.consume()
	if p.lexErr != nil {
		return "", p.lexErr
	}

	if token == "" {
		return "", p.errorf("expected type, got EOF")
	}
	if !unicode.IsLetter(rune(token[0])) {
		return "", p.errorf("invalid type '%s'", token)
	}
	if !validTypeIdentifiers[token] {
		return "", p.errorf("unknown type '%s'", token)
	}
	return token, nil
}

const (
	exprBPNone       = 0
	exprBPTernary    = 10
	exprBPLogicalOr  = 20
	exprBPLogicalAnd = 30
	exprBPCompare    = 40
	exprBPAddSub     = 50
	exprBPMulDiv     = 60
	exprBPPrefix     = 70
)

type decodedStringChar struct {
	b       byte
	escaped bool
}

// parseExpression parses expressions using Pratt/TDOP precedence climbing.
func (p *Parser) parseExpression(rbp int) (ExpressionNode, error) {
	if p.lexErr != nil {
		return nil, p.lexErr
	}

	token := p.consume()
	if p.lexErr != nil {
		return nil, p.lexErr
	}

	if token == "" {
		return nil, p.errorf("unexpected EOF in expression")
	}

	left, err := p.nud(token)
	if err != nil {
		return nil, err
	}

	for {
		next := p.peek()
		if rbp >= p.bindingPower(next) {
			break
		}

		op := p.consume()
		left, err = p.led(op, left)
		if err != nil {
			return nil, err
		}
	}

	return left, nil
}

func (p *Parser) nud(token string) (ExpressionNode, error) {
	switch token {
	case "[":
		elements := make([]ExpressionNode, 0)
		if p.peek() == "]" {
			p.consume()
			return &ArrayExpression{Elements: elements}, nil
		}
		for {
			elem, err := p.parseExpression(exprBPNone)
			if err != nil {
				return nil, err
			}
			elements = append(elements, elem)

			next := p.peek()
			if next == "," {
				p.consume()
				if p.peek() == "]" {
					return nil, p.errorf("expected expression after ',' in array literal")
				}
				continue
			}
			if next == "]" {
				p.consume()
				break
			}
			return nil, p.errorf("expected ',' or ']' in array literal, got '%s'", next)
		}
		return &ArrayExpression{Elements: elements}, nil
	case "(":
		expr, err := p.parseExpression(exprBPNone)
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return expr, nil
	case "-", "!":
		operand, err := p.parseExpression(exprBPPrefix)
		if err != nil {
			return nil, err
		}
		return &UnaryExpression{Operator: token, Operand: operand}, nil
	case "true", "false":
		val, _ := strconv.ParseBool(token)
		return &LiteralExpression{Value: val}, nil
	}

	if _, err := strconv.ParseFloat(token, 64); err == nil {
		val, _ := strconv.ParseFloat(token, 64)
		return &LiteralExpression{Value: val}, nil
	}

	if strings.HasPrefix(token, "\"") {
		if p.disableInterpolation {
			s, err := parseStringLiteral(token)
			if err != nil {
				return nil, p.errorf("%v", err)
			}
			return &LiteralExpression{Value: s}, nil
		}
		s, segments, hasInterpolation, err := parseStringLiteralAndInterpolation(token)
		if err != nil {
			return nil, p.errorf("%v", err)
		}
		if hasInterpolation {
			return &InterpolatedStringExpression{Segments: segments}, nil
		}
		return &LiteralExpression{Value: s}, nil
	}

	if unicode.IsLetter(rune(token[0])) {
		if p.peek() == "(" {
			p.consume()
			args := []ExpressionNode{}
			if p.peek() != ")" {
				for {
					arg, err := p.parseExpression(exprBPNone)
					if err != nil {
						return nil, err
					}
					args = append(args, arg)

					if p.peek() == "," {
						p.consume()
						continue
					}
					break
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return &FunctionCallExpression{Name: token, Args: args}, nil
		}
		return &IdentifierExpression{Name: token}, nil
	}

	return nil, p.errorf("unexpected token in expression: '%s'", token)
}

func (p *Parser) led(op string, left ExpressionNode) (ExpressionNode, error) {
	switch op {
	case "+", "-", "*", "/", "==", "!=", "<", "<=", ">", ">=", "&&", "||":
		rbp := p.bindingPower(op)
		right, err := p.parseExpression(rbp)
		if err != nil {
			return nil, err
		}
		return &BinaryExpression{Left: left, Operator: op, Right: right}, nil
	case "?":
		trueExpr, err := p.parseExpression(exprBPNone)
		if err != nil {
			return nil, err
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		// Right-associative ternary: parse the else branch at a slightly lower threshold.
		falseExpr, err := p.parseExpression(exprBPTernary - 1)
		if err != nil {
			return nil, err
		}
		return &TernaryExpression{
			Condition: left,
			TrueExpr:  trueExpr,
			FalseExpr: falseExpr,
		}, nil
	default:
		return nil, p.errorf("unexpected operator '%s' in expression", op)
	}
}

func (p *Parser) bindingPower(token string) int {
	switch token {
	case "?":
		return exprBPTernary
	case "||":
		return exprBPLogicalOr
	case "&&":
		return exprBPLogicalAnd
	case "==", "!=", "<", ">", "<=", ">=":
		return exprBPCompare
	case "+", "-":
		return exprBPAddSub
	case "*", "/":
		return exprBPMulDiv
	default:
		return exprBPNone
	}
}

// --- Lexer / Helper Methods ---

func (p *Parser) expect(expected string) error {
	if p.lexErr != nil {
		return p.lexErr
	}

	got := p.consume()
	if p.lexErr != nil {
		return p.lexErr
	}

	if got != expected {
		return p.errorf("expected '%s', got '%s'", expected, got)
	}
	return nil
}

func (p *Parser) peek() string {
	p.skipIgnored()
	if p.lexErr != nil {
		return ""
	}

	// Save state
	savedPos, savedLine, savedCol := p.pos, p.line, p.col
	token := p.scan()
	// Restore state
	p.pos, p.line, p.col = savedPos, savedLine, savedCol
	return token
}

func (p *Parser) consume() string {
	p.skipIgnored()
	if p.lexErr != nil {
		return ""
	}

	return p.scan()
}

func (p *Parser) scan() string {
	if p.pos >= len(p.input) {
		return ""
	}

	ch := p.input[p.pos]

	// Check for multi-char operators first
	if p.pos+1 < len(p.input) {
		twoChars := string(p.input[p.pos : p.pos+2])
		switch twoChars {
		case "==", "!=", ">=", "<=", "&&", "||":
			p.pos += 2
			p.col += 2
			return twoChars
		}
	}

	// Handle single-char symbols
	if isSymbolRune(ch) {
		p.pos++
		p.col++
		return string(ch)
	}

	// Handle string literals
	if ch == '"' {
		start := p.pos
		p.pos++
		p.col++
		escaped := false
		for p.pos < len(p.input) {
			curr := p.input[p.pos]
			if escaped {
				escaped = false
				if curr == '\n' {
					p.line++
					p.col = 1
				} else {
					p.col++
				}
				p.pos++
				continue
			}

			if curr == '\\' {
				escaped = true
				p.pos++
				p.col++
				continue
			}

			if curr == '"' {
				p.pos++
				p.col++
				break
			}
			if curr == '\n' {
				p.line++
				p.col = 1
			} else {
				p.col++
			}
			p.pos++
		}
		return string(p.input[start:p.pos])
	}

	// Handle identifiers and numbers
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if unicode.IsSpace(ch) || isSymbolRune(ch) {
			break
		}
		p.pos++
		p.col++
	}
	return string(p.input[start:p.pos])
}

func (p *Parser) skipIgnored() {
	for p.pos < len(p.input) {
		ch := p.input[p.pos]

		if unicode.IsSpace(ch) {
			if ch == '\n' {
				p.line++
				p.col = 1
			} else {
				p.col++
			}
			p.pos++
			continue
		}

		if ch == '\uFEFF' {
			p.pos++
			p.col++
			continue
		}

		if ch == '/' && p.pos+1 < len(p.input) {
			next := p.input[p.pos+1]
			if next == '/' {
				p.pos += 2
				p.col += 2
				for p.pos < len(p.input) {
					curr := p.input[p.pos]
					if curr == '\n' {
						p.pos++
						p.line++
						p.col = 1
						break
					}
					p.pos++
					p.col++
				}
				continue
			}

			if next == '*' {
				startLine, startCol := p.line, p.col
				p.pos += 2
				p.col += 2
				closed := false

				for p.pos < len(p.input) {
					curr := p.input[p.pos]

					if curr == '\n' {
						p.pos++
						p.line++
						p.col = 1
						continue
					}

					if curr == '*' && p.pos+1 < len(p.input) && p.input[p.pos+1] == '/' {
						p.pos += 2
						p.col += 2
						closed = true
						break
					}

					p.pos++
					p.col++
				}

				if !closed {
					p.lexErr = fmt.Errorf("line %d:%d: lex error: unclosed block comment", startLine, startCol)
				}
				continue
			}
		}

		if p.lexErr != nil {
			return
		}

		if !unicode.IsSpace(ch) {
			break
		}
	}
}

func (p *Parser) isEOF() bool {
	p.skipIgnored()
	if p.lexErr != nil {
		return false
	}

	return p.pos >= len(p.input)
}

func (p *Parser) errorf(format string, args ...interface{}) error {
	return fmt.Errorf("line %d:%d: "+format, append([]interface{}{p.line, p.col}, args...)...)
}

func parseStringLiteral(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("unterminated string literal")
	}

	var b strings.Builder
	for i := 1; i < len(raw)-1; i++ {
		ch := raw[i]
		if ch != '\\' {
			b.WriteByte(ch)
			continue
		}

		if i+1 >= len(raw)-1 {
			return "", fmt.Errorf("unterminated escape sequence in string literal")
		}
		i++
		switch raw[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '{':
			b.WriteByte('{')
		case '}':
			b.WriteByte('}')
		default:
			return "", fmt.Errorf("invalid escape sequence '\\%c' in string literal", raw[i])
		}
	}
	return b.String(), nil
}

func parseStringLiteralAndInterpolation(raw string) (string, []InterpolatedStringSegment, bool, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", nil, false, fmt.Errorf("unterminated string literal")
	}

	decoded := make([]decodedStringChar, 0, len(raw))
	for i := 1; i < len(raw)-1; i++ {
		ch := raw[i]
		if ch != '\\' {
			decoded = append(decoded, decodedStringChar{b: ch})
			continue
		}

		if i+1 >= len(raw)-1 {
			return "", nil, false, fmt.Errorf("unterminated escape sequence in string literal")
		}
		i++
		switch raw[i] {
		case 'n':
			decoded = append(decoded, decodedStringChar{b: '\n', escaped: true})
		case 't':
			decoded = append(decoded, decodedStringChar{b: '\t', escaped: true})
		case '"':
			decoded = append(decoded, decodedStringChar{b: '"', escaped: true})
		case '\\':
			decoded = append(decoded, decodedStringChar{b: '\\', escaped: true})
		case '{':
			decoded = append(decoded, decodedStringChar{b: '{', escaped: true})
		case '}':
			decoded = append(decoded, decodedStringChar{b: '}', escaped: true})
		default:
			return "", nil, false, fmt.Errorf("invalid escape sequence '\\%c' in string literal", raw[i])
		}
	}

	segments, hasInterpolation, err := parseInterpolationSegmentsFromDecoded(decoded)
	if err != nil {
		return "", nil, false, err
	}

	var b strings.Builder
	for _, c := range decoded {
		b.WriteByte(c.b)
	}
	return b.String(), segments, hasInterpolation, nil
}

func parseInterpolationSegments(s string) ([]InterpolatedStringSegment, bool, error) {
	decoded := make([]decodedStringChar, 0, len(s))
	for i := 0; i < len(s); i++ {
		decoded = append(decoded, decodedStringChar{b: s[i]})
	}
	return parseInterpolationSegmentsFromDecoded(decoded)
}

func parseInterpolationSegmentsFromDecoded(decoded []decodedStringChar) ([]InterpolatedStringSegment, bool, error) {
	segments := make([]InterpolatedStringSegment, 0)
	hasInterpolation := false

	var literal strings.Builder
	for i := 0; i < len(decoded); i++ {
		ch := decoded[i]
		if ch.b == '}' && !ch.escaped {
			return nil, false, fmt.Errorf("malformed interpolation placeholder: unexpected '}'")
		}
		if ch.b != '{' || ch.escaped {
			literal.WriteByte(ch.b)
			continue
		}

		end := -1
		for j := i + 1; j < len(decoded); j++ {
			if decoded[j].b == '}' && !decoded[j].escaped {
				end = j
				break
			}
		}
		if end < 0 {
			return nil, false, fmt.Errorf("malformed interpolation placeholder: missing closing '}'")
		}
		innerBytes := make([]byte, 0, end-i-1)
		for j := i + 1; j < end; j++ {
			innerBytes = append(innerBytes, decoded[j].b)
		}
		inner := string(innerBytes)

		if literal.Len() > 0 {
			segments = append(segments, InterpolatedStringSegment{Text: literal.String()})
			literal.Reset()
		}

		if !strings.HasPrefix(inner, "param:") {
			return nil, false, fmt.Errorf("unsupported interpolation placeholder '{%s}'", inner)
		}

		name := strings.TrimPrefix(inner, "param:")
		if name == "" {
			return nil, false, fmt.Errorf("malformed interpolation placeholder: empty parameter name")
		}
		if !isValidInterpolationIdentifier(name) {
			return nil, false, fmt.Errorf("malformed interpolation placeholder: invalid parameter name '%s'", name)
		}

		segments = append(segments, InterpolatedStringSegment{ParamName: name})
		hasInterpolation = true
		i = end
	}

	if literal.Len() > 0 || len(segments) == 0 {
		segments = append(segments, InterpolatedStringSegment{Text: literal.String()})
	}
	return segments, hasInterpolation, nil
}

func isValidInterpolationIdentifier(name string) bool {
	if name == "" {
		return false
	}

	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}

	return true
}
