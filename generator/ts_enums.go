package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

func writeTSEnum(g *protogen.GeneratedFile, e *DomainEnum, opts *Options) {
	if e.Comment != "" {
		g.P("/** ", sanitizeJSDoc(e.Comment), " */")
	}

	// Build quoted value list using proto names (e.g. "USER_STATUS_ACTIVE")
	// to match protojson serialization format.
	vals := make([]string, 0, len(e.Values))
	for _, v := range e.Values {
		vals = append(vals, fmt.Sprintf(`"%s"`, v.ProtoName))
	}

	if opts.TSTypesOnly {
		// Types-only mode: emit pure type alias, no Zod.
		if opts.TSEnumStyle == "native" {
			g.P("export const ", e.Name, " = {")
			for _, v := range e.Values {
				g.P(fmt.Sprintf("  %s: %q,", tsSafeKey(v.Name), v.ProtoName))
			}
			g.P("} as const;")
			g.P("export type ", e.Name, " = (typeof ", e.Name, ")[keyof typeof ", e.Name, "];")
		} else {
			g.P("export type ", e.Name, " = ", strings.Join(vals, " | "), " | (string & {});")
		}
		return
	}

	if opts.TSEnumStyle == "native" {
		// Native enum: const object + z.nativeEnum.
		// Keys use stripped PascalCase names, values use proto names for JSON interop.
		g.P("export const ", e.Name, " = {")
		for _, v := range e.Values {
			g.P(fmt.Sprintf("  %s: %q,", tsSafeKey(v.Name), v.ProtoName))
		}
		g.P("} as const;")
		g.P("export const ", e.Name, "Schema = /* @__PURE__ */ z.nativeEnum(", e.Name, ");")
		g.P("export type ", e.Name, " = z.infer<typeof ", e.Name, "Schema>;")
	} else {
		// Default: z.enum with explicit type to avoid z.infer collapse on .or(z.string()).
		g.P("export type ", e.Name, " = ", strings.Join(vals, " | "), " | (string & {});")
		g.P("// Note: Numeric enum values should be converted to strings before passing to this schema.")
		g.P("export const ", e.Name, "Schema = /* @__PURE__ */ z.enum([", strings.Join(vals, ", "), "]).or(z.string());")
	}
}
