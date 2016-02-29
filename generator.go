package jsval

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Generator is responsible for generating Go code that
// sets up a validator
type Generator struct{}

// NewGenerator creates a new Generator
func NewGenerator() *Generator {
	return &Generator{}
}

// Process takes a validator and prints out Go code to out.
func (g *Generator) Process(out io.Writer, validators ...*JSVal) error {
	ctx := genctx{
		pkgname:  "jsval",
		refnames: make(map[string]string),
		vname:    "V",
	}

	// First get all of the references so we can refer to it later
	refs := map[string]Constraint{}
	refnames := []string{}
	for i, v := range validators {
		for rname, rc := range v.refs {
			if _, ok := refs[rname]; ok {
				continue
			}
			refs[rname] = rc
			refnames = append(refnames, rname)
		}

		fmt.Fprintf(out, "var V%d *%s.JSVal", i, ctx.pkgname)
	}

	ctx.refs = refs
	if len(refs) > 0 { // have refs
		ctx.cmname = "M"
		// sort them by reference name
		sort.Strings(refnames)
		fmt.Fprintf(out, "\n%svar %s *%s.ConstraintMap", ctx.Prefix(), ctx.cmname, ctx.pkgname)

		// Generate reference constraint names
		for i, rname := range refnames {
			vname := fmt.Sprintf("R%d", i)
			ctx.refnames[rname] = vname
			fmt.Fprintf(out, "\nvar %s %s.Constraint", vname, ctx.pkgname)
		}
	}

	fmt.Fprintf(out, "\nfunc init() {")
	g1 := ctx.Indent()
	defer g1()
	if len(refs) > 0 {
		fmt.Fprintf(out, "\n%s%s = &%s.ConstraintMap{}", ctx.Prefix(), ctx.cmname, ctx.pkgname)
		// Now generate code for references
		for _, rname := range refnames {
			fmt.Fprintf(out, "\n%s%s = ", ctx.Prefix(), ctx.refnames[rname])
			if err := generateCode(&ctx, out, ctx.refs[rname]); err != nil {
				return err
			}
		}

		p := ctx.Prefix()
		for _, rname := range refnames {
			fmt.Fprintf(out, "\n%s%s.SetReference(`%s`, %s)", p, ctx.cmname, rname, ctx.refnames[rname])
		}
	}

/*

	fmt.Fprintf(out, "var (")
	for i, rname := range refnames {
		vname := fmt.Sprintf("R%d", i)
		ctx.refnames[rname] = vname
		fmt.Fprintf(out, "\n\t// %s is the constraint for %s\n\t%s %s.Constraint", vname, rname, vname, ctx.pkgname)
	}
	for i := range validators {
		fmt.Fprintf(out, "\n\tV%d *%s.JSVal", i, ctx.pkgname)
	}
	fmt.Fprintf(out, "\n)")

	fmt.Fprintf(out, "\nfunc init() {")
	g1 := ctx.Indent()
	defer g1()
	for i, rname := range refnames {
		vname := fmt.Sprintf("R%d", i)
		fmt.Fprintf(out, "\n%s%s = ", ctx.Prefix(), vname)
		if err := generateCode(&ctx, out, refs[rname]); err != nil {
			return err
		}
	}
*/
	// Now dump the validators
	for i, v := range validators {
		vname := fmt.Sprintf("V%d", i)
		fmt.Fprintf(out, "\n%s%s = ", ctx.Prefix(), vname)
		ctx.vname = vname
		if err := generateCode(&ctx, out, v); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "\n}")
	return nil
}

type genctx struct {
	cmname string
	prefix   []byte
	pkgname  string
	refs     map[string]Constraint
	refnames map[string]string
	vname    string
}

func (ctx *genctx) Prefix() []byte {
	return ctx.prefix
}

func (ctx *genctx) Indent() func() {
	ctx.prefix = append(ctx.prefix, '\t')
	return func() {
		l := len(ctx.prefix)
		if l == 0 {
			return
		}
		ctx.prefix = ctx.prefix[:l-1]
	}
}

func generateNilCode(ctx *genctx, out io.Writer, c emptyConstraint) error {
	fmt.Fprintf(out, "%s%s.EmptyConstraint", ctx.Prefix(), ctx.pkgname)
	return nil
}
func generateValidatorCode(ctx *genctx, out io.Writer, v *JSVal) error {
	found := false
	fmt.Fprintf(out, "%s.New()", ctx.pkgname)
	g := ctx.Indent()
	defer g()

	p := ctx.Prefix()

	if cmname := ctx.cmname; cmname != "" {
		fmt.Fprintf(out, ".\n%sSetConstraintMap(%s)", p, cmname)
	}

	for rname, rc := range ctx.refs {
		if v.root == rc {
			fmt.Fprintf(out, ".\n%sSetRoot(%s)", p, ctx.refnames[rname])
			found = true
			break
		}
	}

	if !found {
		fmt.Fprintf(out, ".\n%sSetRoot(\n", p)
		g := ctx.Indent()
		if err := generateCode(ctx, out, v.root); err != nil {
			g()
			return err
		}
		g()
		fmt.Fprintf(out, ",\n%s)\n", p)
	}

/*
	refs := make([]string, 0, v.Len())
	for ref := range v.refs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	for _, ref := range refs {
		fmt.Fprintf(out, ".\n%sSetReference(`%s`, %s)", p, ref, ctx.refnames[ref])
	}
*/
	return nil
}

func generateCode(ctx *genctx, out io.Writer, c interface {
	Validate(interface{}) error
}) error {
	buf := &bytes.Buffer{}

	switch c.(type) {
	case emptyConstraint:
		if err := generateNilCode(ctx, buf, c.(emptyConstraint)); err != nil {
			return err
		}
	case *JSVal:
		if err := generateValidatorCode(ctx, buf, c.(*JSVal)); err != nil {
			return err
		}
	case *AnyConstraint:
		if err := generateAnyCode(ctx, buf, c.(*AnyConstraint)); err != nil {
			return err
		}
	case *AllConstraint:
		if err := generateAllCode(ctx, buf, c.(*AllConstraint)); err != nil {
			return err
		}
	case *ArrayConstraint:
		if err := generateArrayCode(ctx, buf, c.(*ArrayConstraint)); err != nil {
			return err
		}
	case *BooleanConstraint:
		if err := generateBooleanCode(ctx, buf, c.(*BooleanConstraint)); err != nil {
			return err
		}
	case *IntegerConstraint:
		if err := generateIntegerCode(ctx, buf, c.(*IntegerConstraint)); err != nil {
			return err
		}
	case *NotConstraint:
		if err := generateNotCode(ctx, buf, c.(*NotConstraint)); err != nil {
			return err
		}
	case *NumberConstraint:
		if err := generateNumberCode(ctx, buf, c.(*NumberConstraint)); err != nil {
			return err
		}
	case *ObjectConstraint:
		if err := generateObjectCode(ctx, buf, c.(*ObjectConstraint)); err != nil {
			return err
		}
	case *OneOfConstraint:
		if err := generateOneOfCode(ctx, buf, c.(*OneOfConstraint)); err != nil {
			return err
		}
	case *ReferenceConstraint:
		if err := generateReferenceCode(ctx, buf, c.(*ReferenceConstraint)); err != nil {
			return err
		}
	case *StringConstraint:
		if err := generateStringCode(ctx, buf, c.(*StringConstraint)); err != nil {
			return err
		}
	}

	s := buf.String()
	s = strings.TrimSuffix(s, ".\n")
	fmt.Fprintf(out, s)

	return nil
}

func generateReferenceCode(ctx *genctx, out io.Writer, c *ReferenceConstraint) error {
	fmt.Fprintf(out, "%s%s.Reference(%s).RefersTo(`%s`)", ctx.Prefix(), ctx.pkgname, ctx.cmname, c.reference)

	return nil
}

func generateComboCode(ctx *genctx, out io.Writer, name string, clist []Constraint) error {
	if len(clist) == 0 {
		return generateNilCode(ctx, out, EmptyConstraint)
	}
	p := ctx.Prefix()
	fmt.Fprintf(out, "%s%s.%s()", p, ctx.pkgname, name)
	for _, c1 := range clist {
		g1 := ctx.Indent()
		fmt.Fprintf(out, ".\n%sAdd(\n", ctx.Prefix())
		g2 := ctx.Indent()
		if err := generateCode(ctx, out, c1); err != nil {
			g2()
			g1()
			return err
		}
		g2()
		fmt.Fprintf(out, ",\n%s)", ctx.Prefix())
		g1()
	}
	return nil
}

func generateAnyCode(ctx *genctx, out io.Writer, c *AnyConstraint) error {
	return generateComboCode(ctx, out, "Any", c.constraints)
}

func generateAllCode(ctx *genctx, out io.Writer, c *AllConstraint) error {
	return generateComboCode(ctx, out, "All", c.constraints)
}

func generateOneOfCode(ctx *genctx, out io.Writer, c *OneOfConstraint) error {
	return generateComboCode(ctx, out, "OneOf", c.constraints)
}

func generateIntegerCode(ctx *genctx, out io.Writer, c *IntegerConstraint) error {
	fmt.Fprintf(out, "%s%s.Integer()", ctx.Prefix(), ctx.pkgname)

	if c.applyMinimum {
		fmt.Fprintf(out, ".Minimum(%d)", int(c.minimum))
	}

	if c.applyMaximum {
		fmt.Fprintf(out, ".Maximum(%d)", int(c.maximum))
	}

	return nil
}

func generateNumberCode(ctx *genctx, out io.Writer, c *NumberConstraint) error {
	fmt.Fprintf(out, "%s%s.Number()", ctx.Prefix(), ctx.pkgname)

	if c.applyMinimum {
		fmt.Fprintf(out, ".Minimum(%f)", c.minimum)
	}

	if c.exclusiveMinimum {
		fmt.Fprintf(out, ".ExclusiveMinimum(true)")
	}

	if c.applyMaximum {
		fmt.Fprintf(out, ".Maximum(%f)", c.maximum)
	}

	if c.exclusiveMaximum {
		fmt.Fprintf(out, ".ExclusiveMaximum(true)")
	}

	if c.HasDefault() {
		fmt.Fprintf(out, ".Default(%f)", c.DefaultValue())
	}

	return nil
}

func generateEnumCode(ctx *genctx, out io.Writer, c *EnumConstraint) error {
	fmt.Fprintf(out, "[]interface{}{")
	l := len(c.enums)
	for i, v := range c.enums {
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.String:
			fmt.Fprintf(out, "%s", strconv.Quote(rv.String()))
		}
		if i < l-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	fmt.Fprintf(out, "}")

	return nil
}

func generateStringCode(ctx *genctx, out io.Writer, c *StringConstraint) error {
	fmt.Fprintf(out, "%s%s.String()", ctx.Prefix(), ctx.pkgname)

	if c.maxLength > -1 {
		fmt.Fprintf(out, ".MaxLength(%d)", c.maxLength)
	}

	if c.minLength > 0 {
		fmt.Fprintf(out, ".MinLength(%d)", c.minLength)
	}

	if f := c.format; f != "" {
		fmt.Fprintf(out, ".Format(%s)", strconv.Quote(string(f)))
	}

	if rx := c.regexp; rx != nil {
		fmt.Fprintf(out, ".Regexp(`%s`)", rx.String())
	}

	if enum := c.enums; enum != nil {
		fmt.Fprintf(out, ".Enum(")
		if err := generateEnumCode(ctx, out, enum); err != nil {
			return err
		}
		fmt.Fprintf(out, ",)")
	}

	return nil
}

func generateObjectCode(ctx *genctx, out io.Writer, c *ObjectConstraint) error {
	fmt.Fprintf(out, "%s%s.Object()", ctx.Prefix(), ctx.pkgname)

	// object code usually becomes quite nested, so we indent one level
	// to begin with
	g1 := ctx.Indent()
	defer g1()
	p := ctx.Prefix()

	if c.HasDefault() {
		fmt.Fprintf(out, ".\n%sDefault(%s)", p, c.DefaultValue())
	}

	if len(c.required) > 0 {
		fmt.Fprintf(out, ".\n%sRequired([]string{", p)
		l := len(c.required)
		pnames := make([]string, 0, l)
		for pname := range c.required {
			pnames = append(pnames, pname)
		}
		sort.Strings(pnames)
		for i, pname := range pnames {
			fmt.Fprint(out, strconv.Quote(pname))
			if i < l-1 {
				fmt.Fprint(out, ", ")
			}
		}
		fmt.Fprint(out, "})")
	}

	if aprop := c.additionalProperties; aprop != nil {
		fmt.Fprintf(out, ".\n%sAdditionalProperties(\n", p)
		g := ctx.Indent()
		if err := generateCode(ctx, out, aprop); err != nil {
			g()
			return err
		}
		fmt.Fprintf(out, ",\n%s)", p)
		g()
	}

	pnames := make([]string, 0, len(c.properties))
	for pname := range c.properties {
		pnames = append(pnames, pname)
	}
	sort.Strings(pnames)

	for _, pname := range pnames {
		pdef := c.properties[pname]

		g := ctx.Indent()
		fmt.Fprintf(out, ".\n%sAddProp(\n%s\t`%s`,\n", p, p, pname)
		if err := generateCode(ctx, out, pdef); err != nil {
			g()
			return err
		}
		fmt.Fprintf(out, ",\n%s)", p)
		g()
	}

	if m := c.propdeps; len(m) > 0 {
		for from, deplist := range m {
			for _, to := range deplist {
				fmt.Fprintf(out, ".\n%sPropDependency(%s, %s)", ctx.Prefix(), strconv.Quote(from), strconv.Quote(to))
			}
		}
	}

	return nil
}

func generateArrayCode(ctx *genctx, out io.Writer, c *ArrayConstraint) error {
	fmt.Fprintf(out, "%s%s.Array()", ctx.Prefix(), ctx.pkgname)
	if c.minItems > -1 {
		fmt.Fprintf(out, ".MinItems(%d)", c.minItems)
	}
	if c.maxItems > -1 {
		fmt.Fprintf(out, ".MaxItems(%d)", c.maxItems)
	}
	if c.uniqueItems {
		fmt.Fprintf(out, ".UniqueItems(true)")
	}
	return nil
}

func generateBooleanCode(ctx *genctx, out io.Writer, c *BooleanConstraint) error {
	fmt.Fprintf(out, "%s%s.Boolean()", ctx.Prefix(), ctx.pkgname)
	if c.HasDefault() {
		fmt.Fprintf(out, ".Default(%t)", c.DefaultValue())
	}
	return nil
}

func generateNotCode(ctx *genctx, out io.Writer, c *NotConstraint) error {
	fmt.Fprintf(out, "%s%s.Not(\n", ctx.Prefix(), ctx.pkgname)
	g := ctx.Indent()
	if err := generateCode(ctx, out, c.child); err != nil {
		g()
		return err
	}
	g()
	fmt.Fprintf(out, "\n%s)", ctx.Prefix())
	return nil
}
