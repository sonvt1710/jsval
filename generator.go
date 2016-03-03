package jsval

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
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

	buf := bytes.Buffer{}

	// First get all of the references so we can refer to it later
	refs := map[string]Constraint{}
	refnames := []string{}
	valnames := []string{}
	for i, v := range validators {
		for rname, rc := range v.refs {
			if _, ok := refs[rname]; ok {
				continue
			}
			refs[rname] = rc
			refnames = append(refnames, rname)
		}

		if v.Name == "" {
			v.Name = fmt.Sprintf("V%d", i)
		}
		valnames = append(valnames, v.Name)
	}

	sort.Strings(valnames)
	for _, vname := range valnames {
		fmt.Fprintf(&buf, "\nvar %s *%s.JSVal", vname, ctx.pkgname)
	}

	ctx.refs = refs
	if len(refs) > 0 { // have refs
		ctx.cmname = "M"
		// sort them by reference name
		sort.Strings(refnames)
		fmt.Fprintf(&buf, "\nvar %s *%s.ConstraintMap", ctx.cmname, ctx.pkgname)

		// Generate reference constraint names
		for i, rname := range refnames {
			vname := fmt.Sprintf("R%d", i)
			ctx.refnames[rname] = vname
			fmt.Fprintf(&buf, "\nvar %s %s.Constraint", vname, ctx.pkgname)
		}
	}

	fmt.Fprintf(&buf, "\nfunc init() {")
	if len(refs) > 0 {
		fmt.Fprintf(&buf, "\n%s = &%s.ConstraintMap{}", ctx.cmname, ctx.pkgname)
		// Now generate code for references
		for _, rname := range refnames {
			fmt.Fprintf(&buf, "\n%s = ", ctx.refnames[rname])
			rbuf := bytes.Buffer{}
			if err := generateCode(&ctx, &rbuf, ctx.refs[rname]); err != nil {
				return err
			}
			// Remove indentation here
			rs := rbuf.String()
			for i, r := range rs {
				if !unicode.IsSpace(r) {
					rs = rs[i:]
					break
				}
			}
			fmt.Fprint(&buf, rs)
		}

		for _, rname := range refnames {
			fmt.Fprintf(&buf, "\n%s.SetReference(`%s`, %s)", ctx.cmname, rname, ctx.refnames[rname])
		}
	}

	// Now dump the validators
	for _, v := range validators {
		fmt.Fprintf(&buf, "\n%s = ", v.Name)
		if err := generateCode(&ctx, &buf, v); err != nil {
			return err
		}
	}
	fmt.Fprintf(&buf, "\n}")

	fsrc, err := format.Source(buf.Bytes())
	if err != nil {
		os.Stderr.Write(buf.Bytes())
		return err
	}
	out.Write(fsrc)
	return nil
}

type genctx struct {
	cmname   string
	pkgname  string
	refs     map[string]Constraint
	refnames map[string]string
	vname    string
}

func generateNilCode(ctx *genctx, out io.Writer, c emptyConstraint) error {
	fmt.Fprintf(out, "%s.EmptyConstraint", ctx.pkgname)
	return nil
}
func generateValidatorCode(ctx *genctx, out io.Writer, v *JSVal) error {
	found := false
	fmt.Fprintf(out, "%s.New()", ctx.pkgname)

	if cmname := ctx.cmname; cmname != "" {
		fmt.Fprintf(out, ".\nSetConstraintMap(%s)", cmname)
	}

	for rname, rc := range ctx.refs {
		if v.root == rc {
			fmt.Fprintf(out, ".\nSetRoot(%s)", ctx.refnames[rname])
			found = true
			break
		}
	}

	if !found {
		fmt.Fprint(out, ".\nSetRoot(\n")
		if err := generateCode(ctx, out, v.root); err != nil {
			return err
		}
		fmt.Fprint(out, ",\n)\n")
	}

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
	fmt.Fprintf(out, "%s.Reference(%s).RefersTo(`%s`)", ctx.pkgname, ctx.cmname, c.reference)

	return nil
}

func generateComboCode(ctx *genctx, out io.Writer, name string, clist []Constraint) error {
	if len(clist) == 0 {
		return generateNilCode(ctx, out, EmptyConstraint)
	}
	fmt.Fprintf(out, "%s.%s()", ctx.pkgname, name)
	for _, c1 := range clist {
		fmt.Fprint(out, ".\nAdd(\n")
		if err := generateCode(ctx, out, c1); err != nil {
			return err
		}
		fmt.Fprint(out, ",\n)")
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
	fmt.Fprintf(out, "%s.Integer()", ctx.pkgname)

	if c.applyMinimum {
		fmt.Fprintf(out, ".Minimum(%d)", int(c.minimum))
	}

	if c.applyMaximum {
		fmt.Fprintf(out, ".Maximum(%d)", int(c.maximum))
	}

	return nil
}

func generateNumberCode(ctx *genctx, out io.Writer, c *NumberConstraint) error {
	fmt.Fprintf(out, "%s.Number()", ctx.pkgname)

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
	fmt.Fprintf(out, "")
	l := len(c.enums)
	for i, v := range c.enums {
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.String:
			fmt.Fprintf(out, "%s", strconv.Quote(rv.String()))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fmt.Fprintf(out, "%d", rv.Int())
		case reflect.Float32, reflect.Float64:
			fmt.Fprintf(out, "%f", rv.Float())
		default:
			return fmt.Errorf("failed to stringify enum value %#v", rv.Interface())
		}
		if i < l-1 {
			fmt.Fprintf(out, ", ")
		}
	}

	return nil
}

func generateStringCode(ctx *genctx, out io.Writer, c *StringConstraint) error {
	fmt.Fprintf(out, "%s.String()", ctx.pkgname)

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
		fmt.Fprintf(out, ".RegexpString(`%s`)", rx.String())
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
	fmt.Fprintf(out, "%s.Object()", ctx.pkgname)

	if c.HasDefault() {
		fmt.Fprintf(out, ".\nDefault(%s)", c.DefaultValue())
	}

	if len(c.required) > 0 {
		fmt.Fprint(out, ".\nRequired(")
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
		fmt.Fprint(out, ")")
	}

	if aprop := c.additionalProperties; aprop != nil {
		fmt.Fprintf(out, ".\nAdditionalProperties(\n")
		if err := generateCode(ctx, out, aprop); err != nil {
			return err
		}
		fmt.Fprintf(out, ",\n)")
	}

	pnames := make([]string, 0, len(c.properties))
	for pname := range c.properties {
		pnames = append(pnames, pname)
	}
	sort.Strings(pnames)

	for _, pname := range pnames {
		pdef := c.properties[pname]

		fmt.Fprintf(out, ".\nAddProp(\n`%s`,\n", pname)
		if err := generateCode(ctx, out, pdef); err != nil {
			return err
		}
		fmt.Fprint(out, ",\n)")
	}

	if m := c.propdeps; len(m) > 0 {
		for from, deplist := range m {
			for _, to := range deplist {
				fmt.Fprintf(out, ".\nPropDependency(%s, %s)", strconv.Quote(from), strconv.Quote(to))
			}
		}
	}

	return nil
}

func generateArrayCode(ctx *genctx, out io.Writer, c *ArrayConstraint) error {
	fmt.Fprintf(out, "%s.Array()", ctx.pkgname)

	if cc := c.items; cc != nil {
		fmt.Fprint(out, ".\nItems(\n")
		if err := generateCode(ctx, out, cc); err != nil {
			return err
		}
		fmt.Fprint(out, ",\n)")
	}

	if cc := c.additionalItems; cc != nil {
		fmt.Fprint(out, ".\nAdditionalItems(\n")
		if err := generateCode(ctx, out, cc); err != nil {
			return err
		}
		fmt.Fprintf(out, ",\n)")
	}

	if cc := c.positionalItems; len(cc) > 0 {
		fmt.Fprintf(out, ".\nPositionalItems([]%s.Constraint{\n", ctx.pkgname)
		for _, ccc := range cc {
			if err := generateCode(ctx, out, ccc); err != nil {
			}
			fmt.Fprintf(out, ",\n")
		}
		fmt.Fprint(out, "})")
	}
	if c.minItems > -1 {
		fmt.Fprintf(out, ".\nMinItems(%d)", c.minItems)
	}
	if c.maxItems > -1 {
		fmt.Fprintf(out, ".\nMaxItems(%d)", c.maxItems)
	}
	if c.uniqueItems {
		fmt.Fprint(out, ".\nUniqueItems(true)")
	}
	return nil
}

func generateBooleanCode(ctx *genctx, out io.Writer, c *BooleanConstraint) error {
	fmt.Fprintf(out, "%s.Boolean()", ctx.pkgname)
	if c.HasDefault() {
		fmt.Fprintf(out, ".Default(%t)", c.DefaultValue())
	}
	return nil
}

func generateNotCode(ctx *genctx, out io.Writer, c *NotConstraint) error {
	fmt.Fprintf(out, "%s.Not(\n", ctx.pkgname)
	if err := generateCode(ctx, out, c.child); err != nil {
		return err
	}
	fmt.Fprint(out, "\n)")
	return nil
}
