include Makefile
include_dirs:
	@:
.PHONY: audit-all
audit-all: $(LIBC_OBJS)
	@echo AUDIT_COMPILE_COMPLETE
