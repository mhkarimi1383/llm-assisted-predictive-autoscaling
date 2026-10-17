#!/usr/bin/env bash
# Script to generate Python and Go code from proto files in the protos directory.

set -euo pipefail

SCRIPT_DIR="$(dirname "$0")"
PROTOS_DIR="$SCRIPT_DIR/../protos"
OUTPUT_DIR="$SCRIPT_DIR/.."   # project root

# Check if protoc is installed
if ! command -v protoc &> /dev/null; then
    echo "Error: protoc not found. Please install protoc and the necessary plugins."
    echo "For Python: pip install protobuf"
    echo "For Go: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    echo "For Python gRPC: pip install grpcio-tools"
    exit 1
fi

# Check for Go protobuf plugin
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Warning: protoc-gen-go not found. Go code generation will be skipped."
    echo "Install with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    GENERATE_GO=false
else
    GENERATE_GO=true
fi

# Check for Go gRPC plugin
if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "Warning: protoc-gen-go-grpc not found. Go gRPC code generation will be skipped."
    echo "Install with: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    GENERATE_GO_GRPC=false
else
    GENERATE_GO_GRPC=true
fi

# Ensure the protos directory exists
if [[ ! -d "$PROTOS_DIR" ]]; then
    echo "Error: protos directory not found at $PROTOS_DIR"
    exit 1
fi

# Loop over all .proto files in protos directory
for proto_file in "$PROTOS_DIR"/*.proto; do
    if [[ ! -f "$proto_file" ]]; then
        echo "No .proto files found in $PROTOS_DIR"
        continue
    fi

    # Get just the filename (e.g., data_acquisition.proto)
    proto_filename=$(basename "$proto_file")

    echo "Processing $proto_filename..."

    # Extract the package name from the proto file
    # Assuming the package line looks like: "package something;"
    package=$(grep -E '^\s*package\s+' "$proto_file" | sed -E 's/^\s*package\s+([^;]+);.*$/\1/' | head -n1)
    if [[ -z "$package" ]]; then
        echo "Warning: Could not determine package for $proto_filename. Using filename without extension."
        package="${proto_filename%.proto}"
    fi

    # Generate Python code (place directly in protos/)
    echo "  Generating Python code..."
    protoc --python_out="$PROTOS_DIR" --proto_path="$PROTOS_DIR" "$proto_file"

    # Generate Python gRPC code using grpcio-tools (via python module)
    echo "  Generating Python gRPC code..."
    if ! python -m grpc_tools.protoc --python_out="$PROTOS_DIR" --grpc_python_out="$PROTOS_DIR" --proto_path="$PROTOS_DIR" "$proto_file"; then
        echo "Warning: Failed to generate Python gRPC code for $proto_filename."
        echo "         You may need to install grpcio-tools: pip install grpcio-tools"
    fi

    # Generate Go code if plugin is available
    if [[ "$GENERATE_GO" == true ]]; then
        echo "  Generating Go code..."
        # Set go_package to "protos/<package>" so that:
        # - The Go file is placed in $OUTPUT_DIR/protos/<package>/
        # - The Go package declared in the file is <package>
        protoc --go_out="$OUTPUT_DIR" \
               --go_opt=M"$proto_filename"=protos/"$package" \
               --proto_path="$PROTOS_DIR" \
               "$proto_file"
    fi

    # Generate Go gRPC code if plugin is available
    if [[ "$GENERATE_GO_GRPC" == true ]]; then
        echo "  Generating Go gRPC code..."
        # Set go_package to "protos/<package>" so that:
        # - The Go file is placed in $OUTPUT_DIR/protos/<package>/
        # - The Go package declared in the file is <package>
        protoc --go-grpc_out="$OUTPUT_DIR" \
               --go-grpc_opt=M"$proto_filename"=protos/"$package" \
               --proto_path="$PROTOS_DIR" \
               "$proto_file"
    fi
done

echo "Code generation complete."
echo "Python generated files are in: $PROTOS_DIR"
echo "Python gRPC generated files are also in: $PROTOS_DIR (if successful)"
if [[ "$GENERATE_GO" == true ]]; then
    echo "Go generated files are in: $OUTPUT_DIR/protos/<package>/ for each proto file"
fi
if [[ "$GENERATE_GO_GRPC" == true ]]; then
    echo "Go gRPC generated files are also in: $OUTPUT_DIR/protos/<package>/ for each proto file"
fi