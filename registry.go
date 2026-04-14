package gobspect

// registerBuiltins pre-registers all built-in opaque decoders on ins.
// Called by New() before applying user options, so any RegisterDecoder call
// after New() will override a built-in for that type name.
func registerBuiltins(ins *Inspector) {
	// time.Time: implements GobEncoder (via GobEncode → MarshalBinary).
	// The gob wire type uses CommonType.Name = "Time" (reflect.Type.Name()).
	ins.decoders["Time"] = decodeTime

	// math/big.Int and math/big.Rat both have TypeName="" in the gob wire
	// when encoded directly, because they use pointer receivers and gob sets
	// CommonType.Name = reflect.Type.Name() on the pointer type, which is "".
	// A format-based heuristic distinguishes them at decode time.
	ins.decoders[""] = decodeBigAuto

	// uuid.UUID (github.com/google/uuid and github.com/gofrs/uuid).
	// TypeName will be "uuid.UUID" when the type is encoded via an interface
	// and registered with gob.Register.
	ins.decoders["uuid.UUID"] = decodeUUID

	// math/big.Float: registered under a descriptive key for callers who know
	// the concrete type. Because *big.Float uses a pointer receiver for
	// GobEncode, the wire TypeName is "" just like big.Int and big.Rat, so
	// auto-detection via this key is not possible without explicit registration.
	ins.decoders["math/big.Float"] = decodeBigFloat

	// shopspring/decimal.Decimal: two common import-path aliases are registered
	// so callers can look up the decoder regardless of which alias is in use.
	ins.decoders["decimal.Decimal"] = decodeShopspringDecimal
	ins.decoders["shopspring/decimal.Decimal"] = decodeShopspringDecimal
}
