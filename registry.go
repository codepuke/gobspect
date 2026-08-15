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
	// Registered as an unnamed decoder so user-registered unnamed decoders
	// tried before it can override the behavior for a specific application.
	ins.RegisterUnnamedDecoder(decodeBigAuto)

	// uuid.UUID (github.com/google/uuid and github.com/gofrs/uuid).
	// The gob wire CommonType.Name is the bare type name "UUID"
	// (reflect.Type.Name(), no package qualifier — same as "Time" above).
	// The qualified alias is kept for callers doing explicit lookups.
	ins.decoders["UUID"] = decodeUUID
	ins.decoders["uuid.UUID"] = decodeUUID

	// math/big.Float: registered under a descriptive key for callers who know
	// the concrete type. Because *big.Float uses a pointer receiver for
	// GobEncode, the wire TypeName is "" just like big.Int and big.Rat, so
	// auto-detection via this key is not possible without explicit registration.
	ins.decoders["math/big.Float"] = decodeBigFloat

	// shopspring/decimal.Decimal: the wire CommonType.Name is the bare
	// "Decimal". The qualified aliases are kept for explicit lookups.
	ins.decoders["Decimal"] = decodeShopspringDecimal
	ins.decoders["decimal.Decimal"] = decodeShopspringDecimal
	ins.decoders["shopspring/decimal.Decimal"] = decodeShopspringDecimal

	// net/netip types (BinaryMarshaler). The wire CommonType.Name is the
	// bare type name; qualified aliases kept for explicit lookups.
	ins.decoders["Addr"] = decodeNetipAddr
	ins.decoders["netip.Addr"] = decodeNetipAddr
	ins.decoders["Prefix"] = decodeNetipPrefix
	ins.decoders["netip.Prefix"] = decodeNetipPrefix
	ins.decoders["AddrPort"] = decodeNetipAddrPort
	ins.decoders["netip.AddrPort"] = decodeNetipAddrPort
}
