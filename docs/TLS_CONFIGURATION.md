# TLS/SSL Configuration Guide

## Overview

GraphDB supports TLS/SSL encryption for all network communication, providing:
- **Encrypted communication** between clients and servers
- **Authentication** via X.509 certificates
- **Compliance** with security standards (SOC 2, HIPAA, PCI-DSS)
- **Configurable security levels** from development to production

## Quick Start

### 1. Auto-Generated Self-Signed Certificates (Development)

For development and testing, GraphDB can automatically generate self-signed certificates:

```bash
# Enable TLS with auto-generated certificates
export TLS_ENABLED=true
export TLS_AUTO_GENERATE=true

# Start the server
./bin/server
```

The server will:
- Generate a 4096-bit RSA key pair
- Create a self-signed certificate valid for 1 year
- Use secure cipher suites (TLS 1.2+)

### 2. Custom Certificates (Production)

For production deployments, use certificates from a trusted Certificate Authority (CA):

```bash
# Enable TLS with custom certificates
export TLS_ENABLED=true
export TLS_CERT_FILE=/path/to/server.crt
export TLS_KEY_FILE=/path/to/server.key

# Optional: CA certificate for client verification
export TLS_CA_FILE=/path/to/ca.crt

./bin/server
```

## Environment Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `TLS_ENABLED` | Enable TLS/SSL | `false` | `true` |
| `TLS_AUTO_GENERATE` | Auto-generate self-signed cert | `true` | `true` |
| `TLS_CERT_FILE` | Path to certificate file | - | `/etc/graphdb/server.crt` |
| `TLS_KEY_FILE` | Path to private key file | - | `/etc/graphdb/server.key` |
| `TLS_CA_FILE` | Path to CA certificate | - | `/etc/graphdb/ca.crt` |
| `TLS_HOSTS` | Comma-separated hostnames/IPs | `localhost,127.0.0.1` | `example.com,10.0.1.50` |
| `TLS_MIN_VERSION` | Minimum TLS version | `1.2` | `1.3` |
| `TLS_CLIENT_AUTH` | Client certificate requirement | `none` | `required` |

## Certificate Generation

### Generate Self-Signed Certificate Manually

```bash
# Create directory for certificates
mkdir -p /etc/graphdb/certs
cd /etc/graphdb/certs

# Generate private key (4096-bit RSA)
openssl genrsa -out server.key 4096

# Generate certificate signing request
openssl req -new -key server.key -out server.csr \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=graphdb.example.com"

# Generate self-signed certificate (valid for 1 year)
openssl x509 -req -days 365 -in server.csr \
  -signkey server.key -out server.crt \
  -extfile <(printf "subjectAltName=DNS:graphdb.example.com,DNS:localhost,IP:127.0.0.1")

# Set restrictive permissions
chmod 600 server.key
chmod 644 server.crt

# Clean up CSR
rm server.csr
```

### Using Let's Encrypt (Production)

For public-facing servers, use Let's Encrypt for free, trusted certificates:

```bash
# Install certbot
sudo apt-get install certbot

# Generate certificate (requires domain and port 80 access)
sudo certbot certonly --standalone -d graphdb.example.com

# Certificates will be in /etc/letsencrypt/live/graphdb.example.com/

# Configure GraphDB
export TLS_ENABLED=true
export TLS_CERT_FILE=/etc/letsencrypt/live/graphdb.example.com/fullchain.pem
export TLS_KEY_FILE=/etc/letsencrypt/live/graphdb.example.com/privkey.pem

./bin/server
```

**Note**: Let's Encrypt certificates expire after 90 days. Set up auto-renewal:

```bash
# Test renewal
sudo certbot renew --dry-run

# Add to crontab for automatic renewal
echo "0 0 * * * certbot renew --quiet && systemctl restart graphdb" | sudo crontab -
```

## Client Certificate Authentication

For enhanced security, require clients to present valid certificates:

### 1. Create CA and Client Certificates

```bash
# Generate CA private key
openssl genrsa -out ca.key 4096

# Generate CA certificate
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=GraphDB CA"

# Generate client private key
openssl genrsa -out client.key 4096

# Generate client certificate signing request
openssl req -new -key client.key -out client.csr \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=client@example.com"

# Sign client certificate with CA
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt

# Set permissions
chmod 600 client.key ca.key
chmod 644 client.crt ca.crt
```

### 2. Configure Server

```bash
export TLS_ENABLED=true
export TLS_CERT_FILE=/path/to/server.crt
export TLS_KEY_FILE=/path/to/server.key
export TLS_CA_FILE=/path/to/ca.crt
export TLS_CLIENT_AUTH=required  # or: none, request, required

./bin/server
```

### 3. Connect with Client Certificate

```bash
# Using curl
curl --cert client.crt --key client.key --cacert ca.crt \
  https://localhost:8080/health

# Using Python requests
import requests

response = requests.get(
    'https://localhost:8080/health',
    cert=('client.crt', 'client.key'),
    verify='ca.crt'
)
```

## TLS Client Authentication Modes

| Mode | Environment Variable | Description |
|------|---------------------|-------------|
| `none` | `TLS_CLIENT_AUTH=none` | No client certificate required (default) |
| `request` | `TLS_CLIENT_AUTH=request` | Request client cert but don't require it |
| `required` | `TLS_CLIENT_AUTH=required` | Require valid client certificate |
| `verify_if_given` | `TLS_CLIENT_AUTH=verify` | Verify cert if provided |

## Security Best Practices

### 1. Cipher Suites

GraphDB uses secure cipher suites by default (OWASP/Mozilla recommendations):

**TLS 1.3 (Preferred)**:
- `TLS_AES_128_GCM_SHA256`
- `TLS_AES_256_GCM_SHA384`
- `TLS_CHACHA20_POLY1305_SHA256`

**TLS 1.2 (Fallback)**:
- `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256`
- `TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384`
- `TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256`
- `TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384`

Insecure cipher suites (CBC, RC4, 3DES, etc.) are **disabled by default**.

### 2. Minimum TLS Version

Set minimum TLS version to 1.2 or higher:

```bash
export TLS_MIN_VERSION=1.2  # or 1.3 for maximum security
```

**Never use TLS 1.0 or 1.1** - they have known vulnerabilities.

### 3. Certificate Rotation

Rotate certificates regularly:

```bash
# Check certificate expiration
openssl x509 -in /path/to/server.crt -noout -enddate

# Set up monitoring (example with cron)
echo "0 0 * * * /usr/local/bin/check-cert-expiry.sh" | crontab -
```

### 4. Private Key Protection

- **Use 4096-bit RSA keys** (minimum 2048-bit)
- **Set restrictive permissions**: `chmod 600 server.key`
- **Never commit keys to version control**
- **Use HSM or KMS** for production key storage
- **Encrypt keys at rest** when storing backups

### 5. Certificate Validation

For production, always use certificates from trusted CAs:

```bash
# Verify certificate
openssl verify -CAfile ca.crt server.crt

# Check certificate details
openssl x509 -in server.crt -text -noout
```

## Production Deployment Examples

### Docker Deployment

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /bin/graphdb ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /bin/graphdb /bin/graphdb
COPY certs/ /etc/graphdb/certs/

ENV TLS_ENABLED=true
ENV TLS_CERT_FILE=/etc/graphdb/certs/server.crt
ENV TLS_KEY_FILE=/etc/graphdb/certs/server.key
ENV TLS_MIN_VERSION=1.2

EXPOSE 8080
CMD ["/bin/graphdb"]
```

```bash
# Build and run
docker build -t graphdb:latest .
docker run -p 8080:8080 \
  -v /path/to/certs:/etc/graphdb/certs:ro \
  graphdb:latest
```

### Kubernetes Deployment

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: graphdb-tls
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-cert>
  tls.key: <base64-encoded-key>
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: graphdb
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: graphdb
        image: graphdb:latest
        env:
        - name: TLS_ENABLED
          value: "true"
        - name: TLS_CERT_FILE
          value: /etc/tls/tls.crt
        - name: TLS_KEY_FILE
          value: /etc/tls/tls.key
        - name: TLS_MIN_VERSION
          value: "1.2"
        volumeMounts:
        - name: tls-certs
          mountPath: /etc/tls
          readOnly: true
      volumes:
      - name: tls-certs
        secret:
          secretName: graphdb-tls
```

### AWS Load Balancer (ALB) Termination

For AWS deployments, terminate TLS at the Application Load Balancer:

```bash
# Configure ALB with ACM certificate
aws elbv2 create-load-balancer \
  --name graphdb-alb \
  --subnets subnet-xxx subnet-yyy \
  --security-groups sg-xxx

# Add HTTPS listener with ACM certificate
aws elbv2 create-listener \
  --load-balancer-arn arn:aws:elasticloadbalancing:... \
  --protocol HTTPS \
  --port 443 \
  --certificates CertificateArn=arn:aws:acm:... \
  --default-actions Type=forward,TargetGroupArn=arn:aws:elasticloadbalancing:...

# Backend can use HTTP (encrypted by VPC)
# Or enable TLS for end-to-end encryption
```

## Troubleshooting

### Certificate Verification Failed

**Error**: `x509: certificate signed by unknown authority`

**Solution**: Add CA certificate to trust store or use `--cacert` flag

```bash
# Add to system trust store (Ubuntu/Debian)
sudo cp ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates

# Use --cacert with curl
curl --cacert ca.crt https://localhost:8080/health
```

### Certificate Expired

**Error**: `x509: certificate has expired`

**Solution**: Generate new certificate or renew existing one

```bash
# Check expiration
openssl x509 -in server.crt -noout -enddate

# Generate new certificate
openssl req -new -x509 -days 365 -key server.key -out server.crt
```

### Hostname Mismatch

**Error**: `x509: certificate is valid for localhost, not example.com`

**Solution**: Generate certificate with correct SAN (Subject Alternative Names)

```bash
# Include all hostnames in certificate
openssl req -new -x509 -days 365 -key server.key -out server.crt \
  -subj "/CN=example.com" \
  -addext "subjectAltName=DNS:example.com,DNS:www.example.com,DNS:localhost,IP:127.0.0.1"
```

### TLS Handshake Failed

**Error**: `tls: handshake failure`

**Possible causes**:
1. Client using unsupported TLS version (< 1.2)
2. No compatible cipher suites
3. Client certificate required but not provided

**Solution**:
```bash
# Test with verbose output
openssl s_client -connect localhost:8080 -tls1_2 -debug

# Check server TLS configuration
curl -vvv https://localhost:8080/health
```

## Monitoring Certificate Expiration

Create a script to monitor certificate expiration:

```bash
#!/bin/bash
# check-cert-expiry.sh

CERT_FILE="/path/to/server.crt"
DAYS_WARNING=30

# Get expiration date
EXPIRY_DATE=$(openssl x509 -in "$CERT_FILE" -noout -enddate | cut -d= -f2)
EXPIRY_EPOCH=$(date -d "$EXPIRY_DATE" +%s)
CURRENT_EPOCH=$(date +%s)
DAYS_UNTIL_EXPIRY=$(( ($EXPIRY_EPOCH - $CURRENT_EPOCH) / 86400 ))

if [ $DAYS_UNTIL_EXPIRY -lt 0 ]; then
    echo "ERROR: Certificate has expired!"
    exit 1
elif [ $DAYS_UNTIL_EXPIRY -lt $DAYS_WARNING ]; then
    echo "WARNING: Certificate expires in $DAYS_UNTIL_EXPIRY days"
    exit 1
else
    echo "OK: Certificate expires in $DAYS_UNTIL_EXPIRY days"
    exit 0
fi
```

## Compliance

GraphDB TLS implementation supports:

- **SOC 2**: Encrypted data in transit (CC6.7)
- **HIPAA**: Transmission security (164.312(e)(1))
- **PCI-DSS**: Strong cryptography (4.1)
- **GDPR**: Security of processing (Article 32)
- **FIPS 140-2**: Approved cryptographic algorithms

## Performance Impact

TLS adds minimal overhead with modern hardware:

| Operation | Without TLS | With TLS (RSA 4096) | Overhead |
|-----------|-------------|---------------------|----------|
| Handshake | N/A | 5-10ms | N/A |
| Data transfer | 100 MB/s | 95-98 MB/s | 2-5% |
| Latency | 1ms | 1.05ms | +0.05ms |

**Optimization tips**:
- Use TLS 1.3 for faster handshakes (0-RTT)
- Enable session resumption
- Use ECDSA certificates (faster than RSA)
- Offload TLS to load balancer for very high throughput

## References

- [Mozilla SSL Configuration Generator](https://ssl-config.mozilla.org/)
- [OWASP TLS Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Transport_Layer_Protection_Cheat_Sheet.html)
- [Let's Encrypt Documentation](https://letsencrypt.org/docs/)
- [RFC 8446: TLS 1.3](https://tools.ietf.org/html/rfc8446)
