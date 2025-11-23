#!/bin/bash
# Generate TLS certificates for Mosquitto MQTT broker
# Only generates certificates if they don't already exist

set -e

CERT_DIR="$(cd "$(dirname "$0")" && pwd)/certs"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "Certificate generation script for Mosquitto MQTT broker"
echo "======================================================"
echo ""

# Check if certs directory exists, create if not
if [ ! -d "$CERT_DIR" ]; then
    echo "Creating certs directory: $CERT_DIR"
    mkdir -p "$CERT_DIR"
fi

# Function to check if certificate exists
check_cert() {
    if [ -f "$1" ]; then
        echo -e "${YELLOW}✓${NC} Certificate already exists: $(basename "$1")"
        return 0
    else
        return 1
    fi
}

# Function to generate CA certificate
generate_ca() {
    if check_cert "$CERT_DIR/ca.crt"; then
        return 0
    fi
    
    echo "Generating CA certificate..."
    openssl genrsa -out "$CERT_DIR/ca.key" 2048
    openssl req -new -x509 -days 365 -key "$CERT_DIR/ca.key" \
        -out "$CERT_DIR/ca.crt" \
        -subj "/CN=MQTT-CA/O=LNPay/C=US"
    echo -e "${GREEN}✓${NC} CA certificate generated"
}

# Function to generate server certificate with SANs
generate_server() {
    if check_cert "$CERT_DIR/server.crt"; then
        return 0
    fi
    
    echo "Generating server certificate with Subject Alternative Names (SANs)..."
    
    # Create OpenSSL config file for server certificate with SANs
    SERVER_CONF="$CERT_DIR/server.conf"
    cat > "$SERVER_CONF" <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = mosquitto
O = LNPay
C = US

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = mosquitto
DNS.2 = localhost
DNS.3 = *.mosquitto
IP.1 = 127.0.0.1
IP.2 = ::1
EOF
    
    openssl genrsa -out "$CERT_DIR/server.key" 2048
    openssl req -new -key "$CERT_DIR/server.key" \
        -out "$CERT_DIR/server.csr" \
        -config "$SERVER_CONF"
    openssl x509 -req -in "$CERT_DIR/server.csr" \
        -CA "$CERT_DIR/ca.crt" \
        -CAkey "$CERT_DIR/ca.key" \
        -CAcreateserial \
        -out "$CERT_DIR/server.crt" \
        -days 365 \
        -extensions v3_req \
        -extfile "$SERVER_CONF"
    
    # Clean up config file
    rm -f "$SERVER_CONF"
    
    echo -e "${GREEN}✓${NC} Server certificate generated with SANs (mosquitto, localhost)"
}

# Function to generate edge node certificate with SANs
generate_edge() {
    if check_cert "$CERT_DIR/edge.crt"; then
        return 0
    fi
    
    echo "Generating edge node certificate with Subject Alternative Names (SANs)..."
    
    # Create OpenSSL config file for edge certificate with SANs
    EDGE_CONF="$CERT_DIR/edge.conf"
    cat > "$EDGE_CONF" <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = edge-node
O = LNPay
C = US

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = edge-node
DNS.2 = localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF
    
    openssl genrsa -out "$CERT_DIR/edge.key" 2048
    openssl req -new -key "$CERT_DIR/edge.key" \
        -out "$CERT_DIR/edge.csr" \
        -config "$EDGE_CONF"
    openssl x509 -req -in "$CERT_DIR/edge.csr" \
        -CA "$CERT_DIR/ca.crt" \
        -CAkey "$CERT_DIR/ca.key" \
        -CAcreateserial \
        -out "$CERT_DIR/edge.crt" \
        -days 365 \
        -extensions v3_req \
        -extfile "$EDGE_CONF"
    
    # Clean up config file
    rm -f "$EDGE_CONF"
    
    echo -e "${GREEN}✓${NC} Edge node certificate generated with SANs"
}

# Check if OpenSSL is available
if ! command -v openssl &> /dev/null; then
    echo "Error: openssl is not installed. Please install it first."
    exit 1
fi

# Generate certificates
echo "Checking existing certificates..."
echo ""

generate_ca
generate_server
generate_edge

# Clean up CSR, serial, and config files
echo ""
echo "Cleaning up temporary files..."
rm -f "$CERT_DIR"/*.csr "$CERT_DIR"/*.srl "$CERT_DIR"/*.conf 2>/dev/null || true

echo ""
echo "======================================================"
echo -e "${GREEN}Certificate generation complete!${NC}"
echo ""
echo "Generated certificates:"
echo "  - ca.crt, ca.key (Certificate Authority)"
echo "  - server.crt, server.key (Mosquitto broker)"
echo "  - edge.crt, edge.key (Edge node service)"
echo ""
echo "Certificate location: $CERT_DIR"
echo ""

