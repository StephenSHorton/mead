package bridge

// Handlers live in the meadcore package (the domain layer) because the
// bridge package stays domain-free — it owns wire transport, not what
// the wire transports.
//
// See internal/meadcore/handlers.go for the registered set.
