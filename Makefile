## Formats the code.
format: fix-format fix-imports

## Formats the code.
fix-format:
	${call colored,formatting is running...}
	GOTOOLCHAIN=auto go vet ./...
	GOTOOLCHAIN=auto go fmt ./...

## Fix-imports order.
fix-imports:
	${call colored,fixing imports...}
	./scripts/fix-imports-order.sh

.PHONY: \
	format \
	fix-format \
	fix-imports \
