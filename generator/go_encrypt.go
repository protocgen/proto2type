package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// irNeedsEncrypt returns true if any message (recursively) has encrypted fields.
func irNeedsEncrypt(msgs []*DomainMessage) bool {
	for _, m := range msgs {
		if m.Skip {
			continue
		}
		if m.HasEncryptedFields {
			return true
		}
		if irNeedsEncrypt(m.NestedMessages) {
			return true
		}
	}
	return false
}

// generateGoEncrypt emits the Encryptor interface (once per file) and
// EncryptFields / DecryptFields methods on Firestore storage types.
func generateGoEncrypt(g *protogen.GeneratedFile, df *DomainFile) {
	if !irNeedsEncrypt(df.Messages) {
		return
	}

	// Emit the Encryptor interface once per file.
	g.P("// Encryptor handles field-level encryption and decryption.")
	g.P("// Implementations provide the cryptographic backend (AES-GCM, envelope encryption, KMS, etc.).")
	g.P("type Encryptor interface {")
	g.P("\t// Encrypt encrypts plaintext. scope identifies the tenant/user for key derivation.")
	g.P("\t// fieldName is included as AAD to prevent ciphertext swapping between fields.")
	g.P("\tEncrypt(plaintext, scope, fieldName string) (string, error)")
	g.P("\t// Decrypt decrypts ciphertext with the same scope and fieldName used during encryption.")
	g.P("\tDecrypt(ciphertext, scope, fieldName string) (string, error)")
	g.P("}")
	g.P()
}

// generateGoEncryptMethods emits EncryptFields and DecryptFields on a Firestore type.
func generateGoEncryptMethods(g *protogen.GeneratedFile, dm *DomainMessage, suffix string) {
	if !dm.HasEncryptedFields {
		return
	}

	typeName := dm.Name + suffix
	recv := receiverName(dm.Name)

	// Collect encrypted fields.
	type encField struct {
		pascalName string
		protoName  string
		isPointer  bool // optional fields are *string in Go
	}
	var fields []encField
	for _, f := range dm.Fields {
		if f.Encrypt {
			fields = append(fields, encField{
				pascalName: f.PascalName,
				protoName:  f.Name,
				isPointer:  f.Optional,
			})
		}
	}

	// EncryptFields
	g.P("// EncryptFields encrypts all fields annotated with (proto2type.field).encrypt = true.")
	g.P("// scope is typically the owning user/tenant ID, used as AAD for key derivation.")
	g.P("// Call exactly once before writing to storage. Calling twice will double-encrypt.")
	g.P("func (", recv, " *", typeName, ") EncryptFields(enc Encryptor, scope string) error {")
	g.P("\tvar err error")
	for _, f := range fields {
		if f.isPointer {
			g.P("\tif ", recv, ".", f.pascalName, " != nil && *", recv, ".", f.pascalName, " != \"\" {")
			g.P("\t\tvar encrypted string")
			g.P("\t\tencrypted, err = enc.Encrypt(*", recv, ".", f.pascalName, ", scope, \"", f.protoName, "\")")
		} else {
			g.P("\tif ", recv, ".", f.pascalName, " != \"\" {")
			g.P("\t\t", recv, ".", f.pascalName, ", err = enc.Encrypt(", recv, ".", f.pascalName, ", scope, \"", f.protoName, "\")")
		}
		g.P("\t\tif err != nil {")
		g.P("\t\t\treturn fmt.Errorf(\"encrypting ", f.protoName, ": %w\", err)")
		g.P("\t\t}")
		if f.isPointer {
			g.P("\t\t", recv, ".", f.pascalName, " = &encrypted")
		}
		g.P("\t}")
	}
	g.P("\treturn nil")
	g.P("}")
	g.P()

	// DecryptFields
	g.P("// DecryptFields decrypts all fields annotated with (proto2type.field).encrypt = true.")
	g.P("// scope must match the value used during encryption.")
	g.P("// Call exactly once after reading from storage. Calling twice will corrupt data.")
	g.P("func (", recv, " *", typeName, ") DecryptFields(enc Encryptor, scope string) error {")
	g.P("\tvar err error")
	for _, f := range fields {
		if f.isPointer {
			g.P("\tif ", recv, ".", f.pascalName, " != nil && *", recv, ".", f.pascalName, " != \"\" {")
			g.P("\t\tvar decrypted string")
			g.P("\t\tdecrypted, err = enc.Decrypt(*", recv, ".", f.pascalName, ", scope, \"", f.protoName, "\")")
		} else {
			g.P("\tif ", recv, ".", f.pascalName, " != \"\" {")
			g.P("\t\t", recv, ".", f.pascalName, ", err = enc.Decrypt(", recv, ".", f.pascalName, ", scope, \"", f.protoName, "\")")
		}
		g.P("\t\tif err != nil {")
		g.P("\t\t\treturn fmt.Errorf(\"decrypting ", f.protoName, ": %w\", err)")
		g.P("\t\t}")
		if f.isPointer {
			g.P("\t\t", recv, ".", f.pascalName, " = &decrypted")
		}
		g.P("\t}")
	}
	g.P("\treturn nil")
	g.P("}")
	g.P()
}
