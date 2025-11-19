# Protocol Buffer Library Migration

This document describes the migration from gogo/protobuf to the official google.golang.org/protobuf library.

## Background

etcd previously used gogo/protobuf for protocol buffer serialization. This library is now in maintenance mode and will not receive security patches or new features. To ensure continued security and compatibility with the modern Go ecosystem, we've migrated to the official protobuf library.

## Changes Made

### Proto Files

All `.proto` files have been updated to remove gogo-specific imports and options:

- Removed `import "gogoproto/gogo.proto";`
- Removed file-level options like `(gogoproto.marshaler_all)`, `(gogoproto.sizer_all)`, etc.
- Removed field-level options like `(gogoproto.nullable)`

### Code Generation

The `scripts/genproto.sh` script has been updated to use:
- `protoc-gen-go` instead of `protoc-gen-gofast`
- `protoc-gen-go-grpc` for gRPC service generation

### Go Source Files

Go source files that imported gogo/protobuf have been updated to use google.golang.org/protobuf:

```go
// Before
import "github.com/gogo/protobuf/proto"

// After
import "google.golang.org/protobuf/proto"
```

### Dependencies

The `tools/mod/go.mod` has been updated to:
- Remove `github.com/gogo/protobuf`
- Add `google.golang.org/protobuf` and `google.golang.org/grpc/cmd/protoc-gen-go-grpc`

## Wire Compatibility

The migration maintains binary wire format compatibility. Data written with the old library can be read by the new library and vice versa. This is ensured through:

1. **Same wire encoding**: Protocol buffers use the same binary format regardless of the library
2. **Field number preservation**: All field numbers remain unchanged
3. **Type preservation**: All field types remain unchanged

### Testing Wire Compatibility

Wire compatibility tests have been added to verify that messages can be correctly marshaled and unmarshaled:

- `api/mvccpb/wire_compatibility_test.go`
- `server/storage/wal/walpb/wire_compatibility_test.go`

## API Differences

### proto.Message Interface

The `proto.Message` interface is compatible between libraries, but the concrete types may differ slightly:

- Proto2 optional fields become pointers in the official library
- Proto3 fields without `optional` are scalar values (same as before)

### Marshal/Unmarshal

The API for marshaling and unmarshaling remains the same:

```go
// Both work the same way
data, err := proto.Marshal(msg)
err := proto.Unmarshal(data, msg)
```

## Regenerating Protobuf Files

After making changes to `.proto` files, regenerate the Go code:

```bash
./scripts/genproto.sh
```

This will:
1. Generate `.pb.go` files using the official protobuf compiler
2. Generate `_grpc.pb.go` files for services
3. Generate grpc-gateway files for HTTP endpoints

## Rollback Strategy

If issues are discovered:
1. Revert commits to restore gogo/protobuf
2. Regenerate protobuf files with gogo tools
3. The wire format remains compatible, so no data migration is needed

## References

- [Official protobuf Go API](https://pkg.go.dev/google.golang.org/protobuf)
- [Migration guide](https://go.dev/blog/protobuf-apiv2)
- [gogo/protobuf deprecation](https://github.com/gogo/protobuf/issues/691)
- [RFC-0006: Protocol Buffer Library Migration](../rfcs/RFC-0006-protobuf-library-migration.md)
