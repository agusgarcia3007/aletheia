package answerer

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// EvaluateMath performs real computation for math questions that go beyond the
// two-operand arithmetic path: percentages, powers, square roots, linear
// equations, and general arithmetic expressions with parentheses. It is a
// parser/evaluator, NOT a lookup table — any well-formed expression is solved.
// It returns a natural-language answer and ok=false when the input is not a
// solvable math query.
func EvaluateMath(query string) (string, bool) {
	n := mathNormalize(query)
	if n == "" {
		return "", false
	}
	if ans, ok := solveLinearEquation(n); ok {
		return ans, true
	}
	if ans, ok := solvePercentage(n); ok {
		return ans, true
	}
	if ans, ok := solveSqrt(n); ok {
		return ans, true
	}
	if ans, ok := solvePower(n); ok {
		return ans, true
	}
	if ans, ok := solveExpression(n); ok {
		return ans, true
	}
	return "", false
}

var (
	mathDigitRe   = regexp.MustCompile(`\d`)
	mathPercentRe = regexp.MustCompile(`(-?\d+(?:\.\d+)?)\s*(?:%|por\s*ciento)\s+de\s+(-?\d+(?:\.\d+)?)`)
	mathSqrtRe    = regexp.MustCompile(`raiz\s+cuadrada\s+de\s+(-?\d+(?:\.\d+)?)`)
	mathPowerRe   = regexp.MustCompile(`(-?\d+(?:\.\d+)?)\s*(?:elevado\s+a(?:\s+la)?|\^|a\s+la\s+potencia\s+de)\s*(-?\d+(?:\.\d+)?)`)
	mathExprChars = regexp.MustCompile(`^[0-9+\-*/%^().\s]+$`)
)

// LooksLikeMath reports whether the query is plausibly a math question we can
// attempt to solve. Dictionary-free: it keys on digits plus math operators or a
// small set of math intent words.
func LooksLikeMath(query string) bool {
	n := mathNormalize(query)
	if !mathDigitRe.MatchString(n) {
		return false
	}
	if hasAny(n, "cuanto es", "cuanto da", "calcula", "calcular", "resolve", "resuelve",
		"raiz cuadrada", "elevado", "a la potencia", "por ciento", "%", "ecuacion",
		"+", "-", "*", "/", "^", " por ", " mas ", " menos ", " dividido ", "suma", "resta", "multiplica", "divide") {
		return true
	}

	if mathExprChars.MatchString(strings.TrimSpace(query)) && len(strings.TrimSpace(query)) >= 3 {
		return true
	}

	if strings.Contains(n, "x") && strings.Contains(n, "=") {
		return true
	}
	return false
}

func solvePercentage(n string) (string, bool) {
	m := mathPercentRe.FindStringSubmatch(n)
	if len(m) != 3 {
		return "", false
	}
	p, _ := strconv.ParseFloat(m[1], 64)
	base, _ := strconv.ParseFloat(m[2], 64)
	result := p / 100 * base
	return fmt.Sprintf("El %s%% de %s es %s.", trimNum(p), trimNum(base), trimNum(result)), true
}

func solveSqrt(n string) (string, bool) {
	m := mathSqrtRe.FindStringSubmatch(n)
	if len(m) != 2 {
		return "", false
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	if v < 0 {
		return "No puedo sacar la raíz cuadrada real de un número negativo.", true
	}
	return fmt.Sprintf("La raíz cuadrada de %s es %s.", trimNum(v), trimNum(math.Sqrt(v))), true
}

func solvePower(n string) (string, bool) {
	m := mathPowerRe.FindStringSubmatch(n)
	if len(m) != 3 {
		return "", false
	}
	base, _ := strconv.ParseFloat(m[1], 64)
	exp, _ := strconv.ParseFloat(m[2], 64)
	return fmt.Sprintf("%s elevado a %s es %s.", trimNum(base), trimNum(exp), trimNum(math.Pow(base, exp))), true
}

func solveLinearEquation(n string) (string, bool) {
	if !strings.Contains(n, "=") || !strings.Contains(n, "x") {
		return "", false
	}
	parts := strings.SplitN(n, "=", 2)
	if len(parts) != 2 {
		return "", false
	}
	leftCoeff, leftConst, ok := linearSide(parts[0])
	if !ok {
		return "", false
	}
	rightCoeff, rightConst, ok := linearSide(parts[1])
	if !ok {
		return "", false
	}
	coeff := leftCoeff - rightCoeff
	constant := rightConst - leftConst
	if coeff == 0 {
		return "", false
	}
	x := constant / coeff
	return fmt.Sprintf("x = %s.", trimNum(x)), true
}

// linearSide parses one side of a linear equation into (coefficient of x, constant).
func linearSide(side string) (float64, float64, bool) {
	side = strings.ReplaceAll(side, " ", "")
	if side == "" {
		return 0, 0, false
	}
	// Insert explicit '+' separators before each sign so we can split on terms.
	var terms []string
	cur := strings.Builder{}
	for i, r := range side {
		if (r == '+' || r == '-') && i > 0 {
			terms = append(terms, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		terms = append(terms, cur.String())
	}
	var coeff, constant float64
	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(term, "x") {
			c := strings.ReplaceAll(term, "x", "")
			switch c {
			case "", "+":
				coeff += 1
			case "-":
				coeff -= 1
			default:
				v, err := strconv.ParseFloat(c, 64)
				if err != nil {
					return 0, 0, false
				}
				coeff += v
			}
			continue
		}
		v, err := strconv.ParseFloat(term, 64)
		if err != nil {
			return 0, 0, false
		}
		constant += v
	}
	return coeff, constant, true
}

func solveExpression(n string) (string, bool) {
	expr := wordsToSymbols(n)
	expr = stripMathLeadIn(expr)
	if strings.TrimSpace(expr) == "" || !mathExprChars.MatchString(expr) {
		return "", false
	}
	p := &mathParser{input: []rune(strings.ReplaceAll(expr, " ", ""))}
	value, ok := p.parse()
	if !ok || !p.atEnd() {
		return "", false
	}
	return fmt.Sprintf("El resultado es %s.", trimNum(value)), true
}

// wordsToSymbols converts spanish math words into operators so the expression
// parser can handle phrasings like "12 mas 3 por 2".
func wordsToSymbols(n string) string {
	replacer := strings.NewReplacer(
		" mas ", "+", " menos ", "-", " por ", "*", " dividido por ", "/", " dividido ", "/",
		" entre ", "/", " mod ", "%",
	)
	return replacer.Replace(n)
}

// stripMathLeadIn removes intent words so only the expression remains.
func stripMathLeadIn(expr string) string {
	for _, lead := range []string{"cuanto es", "cuanto da", "calcula", "calcular", "resolve", "resuelve", "el resultado de", "resultado de"} {
		expr = strings.ReplaceAll(expr, lead, " ")
	}
	return strings.TrimSpace(expr)
}

// mathParser is a small recursive-descent evaluator for + - * / % ^ and parens.
type mathParser struct {
	input []rune
	pos   int
}

func (p *mathParser) atEnd() bool { return p.pos >= len(p.input) }

func (p *mathParser) peek() rune {
	if p.atEnd() {
		return 0
	}
	return p.input[p.pos]
}

func (p *mathParser) parse() (float64, bool) { return p.parseAddSub() }

func (p *mathParser) parseAddSub() (float64, bool) {
	left, ok := p.parseMulDiv()
	if !ok {
		return 0, false
	}
	for p.peek() == '+' || p.peek() == '-' {
		op := p.peek()
		p.pos++
		right, ok := p.parseMulDiv()
		if !ok {
			return 0, false
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, true
}

func (p *mathParser) parseMulDiv() (float64, bool) {
	left, ok := p.parsePower()
	if !ok {
		return 0, false
	}
	for p.peek() == '*' || p.peek() == '/' || p.peek() == '%' {
		op := p.peek()
		p.pos++
		right, ok := p.parsePower()
		if !ok {
			return 0, false
		}
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, false
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, false
			}
			left = math.Mod(left, right)
		}
	}
	return left, true
}

func (p *mathParser) parsePower() (float64, bool) {
	base, ok := p.parseUnary()
	if !ok {
		return 0, false
	}
	if p.peek() == '^' {
		p.pos++
		exp, ok := p.parsePower()
		if !ok {
			return 0, false
		}
		return math.Pow(base, exp), true
	}
	return base, true
}

func (p *mathParser) parseUnary() (float64, bool) {
	if p.peek() == '-' {
		p.pos++
		v, ok := p.parseUnary()
		return -v, ok
	}
	if p.peek() == '+' {
		p.pos++
		return p.parseUnary()
	}
	return p.parseAtom()
}

func (p *mathParser) parseAtom() (float64, bool) {
	if p.peek() == '(' {
		p.pos++
		v, ok := p.parseAddSub()
		if !ok || p.peek() != ')' {
			return 0, false
		}
		p.pos++
		return v, true
	}
	start := p.pos
	for !p.atEnd() && (p.input[p.pos] == '.' || (p.input[p.pos] >= '0' && p.input[p.pos] <= '9')) {
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	v, err := strconv.ParseFloat(string(p.input[start:p.pos]), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func mathNormalize(text string) string {
	return normalize(text)
}

func trimNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}
