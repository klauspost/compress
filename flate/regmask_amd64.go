package flate

const (
	// Masks for shifts with register sizes of the shift value.
	// This can be used to work around the x86 design of shifting by mod register size.
	// It can be used when a variable shift is always smaller than the register size.

	// reg8SizeMaskX - shift value is 8 bits, shifted is X
	reg8SizeMask8  = 7
	reg8SizeMask16 = 15
	reg8SizeMask32 = 31
	reg8SizeMask64 = 63

	// regSizeMaskUintX - shift value is uint, shifted is X
	regSizeMaskUint8  = reg8SizeMask8
	regSizeMaskUint16 = reg8SizeMask16
	regSizeMaskUint32 = reg8SizeMask32
	regSizeMaskUint64 = reg8SizeMask64
)
