module github.com/klauspost/compress

go 1.23

retract (
	// https://github.com/klauspost/compress/issues/1114
	v1.18.1

	// https://github.com/klauspost/compress/pull/503
	v1.14.3
	v1.14.2
	v1.14.1
)
