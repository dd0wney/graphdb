# Encryption at Rest Architecture

## Overview

GraphDB Enterprise implements AES-256-GCM encryption for all data at rest, providing confidentiality, integrity, and authenticity guarantees for stored data.

## Design Goals

1. **Strong Encryption**: AES-256-GCM with authenticated encryption
2. **Key Security**: Secure key derivation, storage, and rotation
3. **Performance**: Minimal overhead (<10% performance impact)
4. **Compliance**: Meet FIPS 140-2, GDPR, SOC2, HIPAA requirements
5. **Transparent**: Automatic encryption/decryption without code changes
6. **Flexible**: Support for external key management systems (KMS)

## Architecture Components

### 1. Encryption Layer

```
┌─────────────────────────────────────────────────────────┐
│                     Application Layer                    │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│              Storage Layer (pkg/storage)                 │
│  ┌────────────────────────────────────────────────────┐ │
│  │         Encryption Middleware                       │ │
│  │  - Transparent encrypt on write                    │ │
│  │  - Transparent decrypt on read                     │ │
│  │  - Key rotation support                            │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│           Encrypted Storage (pkg/encryption)             │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Crypto Engine                                      │ │
│  │  - AES-256-GCM encryption/decryption               │ │
│  │  - AEAD (Authenticated Encryption with             │ │
│  │    Associated Data)                                │ │
│  │  - Random nonce generation (96-bit)                │ │
│  └────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Key Manager                                        │ │
│  │  - Master key derivation (PBKDF2)                  │ │
│  │  - Data encryption keys (DEK) per file             │ │
│  │  - Key rotation and versioning                     │ │
│  │  - External KMS integration                        │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                  Disk Storage                            │
│  - Encrypted snapshot files                             │
│  - Encrypted WAL files                                  │
│  - Encrypted edge store files                           │
│  - Key metadata (encrypted)                             │
└─────────────────────────────────────────────────────────┘
```

### 2. Encryption Format

Each encrypted file uses this structure:

```
┌──────────────────────────────────────────────────────────┐
│                    File Header (64 bytes)                 │
├──────────────────────────────────────────────────────────┤
│  Magic Number (8 bytes): "GDBE0001"                      │
│  Version (4 bytes): 0x00000001                           │
│  Algorithm (4 bytes): AES-256-GCM                        │
│  Key Version (4 bytes): Current key version              │
│  Reserved (44 bytes): For future use                     │
└──────────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────────┐
│              Encrypted DEK Block (256 bytes)              │
├──────────────────────────────────────────────────────────┤
│  DEK Nonce (12 bytes): Random nonce for DEK encryption   │
│  Encrypted DEK (32 bytes): Data encryption key           │
│  DEK Tag (16 bytes): Authentication tag for DEK          │
│  Reserved (196 bytes): For future use                    │
└──────────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────────┐
│                 Data Block 1 (Variable)                   │
├──────────────────────────────────────────────────────────┤
│  Nonce (12 bytes): Unique per block                      │
│  Encrypted Data (N bytes): Actual data                   │
│  Tag (16 bytes): Authentication tag                      │
└──────────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────────┐
│                 Data Block 2 (Variable)                   │
├──────────────────────────────────────────────────────────┤
│  Nonce (12 bytes): Unique per block                      │
│  Encrypted Data (N bytes): Actual data                   │
│  Tag (16 bytes): Authentication tag                      │
└──────────────────────────────────────────────────────────┘
│                        ...                                │
```

**Block Size**: 64KB (configurable)
**Why blocks**: Allows random access without decrypting entire file

### 3. Key Hierarchy

```
┌─────────────────────────────────────────────────────────┐
│            Master Encryption Key (MEK)                   │
│  - Derived from passphrase via PBKDF2                   │
│  - 256-bit key                                          │
│  - Never stored on disk in plaintext                    │
│  - Can be provided via env var, config, or KMS          │
└─────────────────────────────────────────────────────────┘
                           │
                           │ Encrypts
                           ▼
┌─────────────────────────────────────────────────────────┐
│       Key Encryption Keys (KEK) - Per Version           │
│  - One per key version (for rotation)                   │
│  - 256-bit key                                          │
│  - Stored encrypted with MEK                            │
│  - Versioned (v1, v2, v3...)                           │
└─────────────────────────────────────────────────────────┘
                           │
                           │ Encrypts
                           ▼
┌─────────────────────────────────────────────────────────┐
│      Data Encryption Keys (DEK) - Per File              │
│  - Unique random key per file                           │
│  - 256-bit key                                          │
│  - Stored encrypted with KEK in file header             │
│  - Generated using crypto/rand                          │
└─────────────────────────────────────────────────────────┘
                           │
                           │ Encrypts
                           ▼
┌─────────────────────────────────────────────────────────┐
│                  Data Blocks                             │
│  - Actual user data                                     │
│  - Each block encrypted with DEK + unique nonce         │
└─────────────────────────────────────────────────────────┘
```

**Benefits of this hierarchy:**
- Master key compromise doesn't expose all data immediately
- Easy key rotation by re-encrypting KEKs
- Per-file DEKs limit blast radius
- Supports multi-tenancy with different KEKs per tenant

### 4. Key Derivation

**Master Key Derivation (from passphrase):**
```
MEK = PBKDF2-HMAC-SHA256(
    passphrase,
    salt (32 bytes, stored in config),
    iterations = 600000,
    keylen = 32
)
```

**Data Encryption Key Generation (per file):**
```
DEK = Random(32 bytes)  // crypto/rand.Read()
```

**Nonce Generation (per block):**
```
Nonce = Random(12 bytes)  // crypto/rand.Read()
```

### 5. Encryption Operations

**Write Path:**
```go
1. Generate random DEK (32 bytes)
2. Encrypt DEK with current KEK → Encrypted DEK
3. Write file header with version, encrypted DEK
4. For each data block:
   a. Generate random nonce (12 bytes)
   b. Encrypt block with AES-256-GCM(DEK, nonce, data)
   c. Write nonce + ciphertext + tag
```

**Read Path:**
```go
1. Read file header
2. Extract key version and encrypted DEK
3. Decrypt DEK with appropriate KEK
4. For each data block:
   a. Read nonce + ciphertext + tag
   b. Decrypt and verify with AES-256-GCM(DEK, nonce, ciphertext, tag)
   c. Return plaintext
```

### 6. Key Rotation

**Rotation Process:**
```
1. Generate new KEK (KEK_v2)
2. Encrypt KEK_v2 with MEK
3. Store KEK_v2 in key store
4. New files use KEK_v2
5. Existing files remain on KEK_v1 (lazy rotation)
6. Background job re-encrypts DEKs with KEK_v2
```

**Rotation Strategies:**
- **Lazy**: Re-encrypt on next write
- **Background**: Scheduled job re-encrypts all files
- **Immediate**: Re-encrypt all files synchronously (for security incidents)

### 7. Performance Optimization

**Techniques:**
1. **Block-level encryption**: Only decrypt accessed blocks
2. **Streaming encryption**: Encrypt/decrypt in chunks (no full file buffering)
3. **Hardware acceleration**: Use AES-NI when available
4. **Parallel processing**: Encrypt/decrypt blocks in parallel
5. **Caching**: Cache decrypted blocks in memory (with secure erasure)

**Expected Performance:**
- Encryption overhead: 5-10% on modern hardware with AES-NI
- Throughput: ~2-4 GB/s on typical server hardware
- Latency: <1ms for small blocks

### 8. Key Management

**Key Storage Options:**

**Option 1: Environment Variable**
```bash
export GRAPHDB_ENCRYPTION_KEY="base64-encoded-key"
```

**Option 2: Configuration File (encrypted)**
```yaml
encryption:
  enabled: true
  key_file: /etc/graphdb/master.key.encrypted
  passphrase_env: GRAPHDB_PASSPHRASE
```

**Option 3: External KMS (AWS KMS, HashiCorp Vault, etc.)**
```yaml
encryption:
  enabled: true
  kms:
    provider: aws-kms
    key_id: arn:aws:kms:us-east-1:123456789:key/abc123
```

**Option 4: Hardware Security Module (HSM)**
```yaml
encryption:
  enabled: true
  hsm:
    provider: pkcs11
    module_path: /usr/lib/libpkcs11.so
    slot: 0
```

### 9. Compliance Features

**GDPR:**
- Data encrypted at rest (Art. 32)
- Key rotation for security (Art. 32)
- Secure deletion via key destruction (Art. 17)

**SOC 2:**
- Encryption of sensitive data (CC6.7)
- Key management procedures (CC6.1)
- Access controls to keys (CC6.2)

**HIPAA:**
- Encryption of ePHI (164.312(a)(2)(iv))
- Key management (164.312(e)(2)(ii))
- Audit logging of key access (164.312(b))

**FIPS 140-2:**
- AES-256 approved algorithm
- PBKDF2 for key derivation
- Cryptographically secure random number generation

### 10. Implementation Phases

**Phase 1: Core Encryption Engine**
- AES-256-GCM implementation
- Block-level encryption/decryption
- File format definition
- Basic key management

**Phase 2: Key Hierarchy**
- MEK/KEK/DEK implementation
- PBKDF2 key derivation
- Key versioning
- Key rotation support

**Phase 3: Storage Integration**
- Transparent encryption in snapshot files
- WAL encryption
- Edge store encryption
- Migration tool for existing data

**Phase 4: External KMS**
- AWS KMS integration
- HashiCorp Vault integration
- Generic KMS interface
- HSM support

**Phase 5: Advanced Features**
- Lazy re-encryption
- Background re-encryption jobs
- Per-tenant keys (multi-tenancy)
- Encryption metrics and monitoring

### 11. Security Considerations

**Threats Mitigated:**
- **Disk theft**: All data encrypted, useless without key
- **Backup compromise**: Backups are encrypted
- **Cold boot attacks**: Keys never in plaintext on disk
- **Memory dumps**: Keys wiped after use

**Threats NOT Mitigated:**
- **Runtime memory access**: Decrypted data in memory
- **Side-channel attacks**: Timing attacks (use constant-time ops)
- **Compromised host**: Attacker with root access can extract keys from memory

**Mitigation Strategies:**
- Use secure memory allocation (mlock, secure erasure)
- Minimize key lifetime in memory
- Use constant-time comparison for tags
- Enable ASLR and other OS-level protections

### 12. Testing Strategy

**Unit Tests:**
- Encryption/decryption correctness
- Key derivation
- Nonce uniqueness
- Tag verification

**Integration Tests:**
- Full file encryption/decryption
- Key rotation
- Multi-version support
- Error handling

**Performance Tests:**
- Encryption/decryption throughput
- Latency measurements
- Memory usage
- Comparison with unencrypted baseline

**Security Tests:**
- Cryptographic primitives validation
- Key zeroization
- Side-channel resistance
- Fuzzing for vulnerabilities

### 13. Configuration Example

```yaml
encryption:
  enabled: true

  # Key source
  master_key:
    source: env  # env, file, kms, hsm
    env_var: GRAPHDB_MASTER_KEY

  # Algorithm settings
  algorithm: aes-256-gcm
  block_size: 65536  # 64KB

  # Key rotation
  rotation:
    enabled: true
    strategy: lazy  # lazy, background, immediate
    schedule: "@weekly"

  # Performance tuning
  performance:
    parallel_blocks: 4
    cache_decrypted_blocks: true
    cache_size_mb: 256

  # Compliance
  compliance:
    fips_mode: true
    audit_key_access: true
    secure_memory: true
```

### 14. Monitoring Metrics

**Encryption Metrics:**
- `graphdb_encryption_operations_total{operation="encrypt|decrypt", status="success|error"}`
- `graphdb_encryption_duration_seconds{operation="encrypt|decrypt"}`
- `graphdb_encryption_bytes_total{direction="encrypted|decrypted"}`
- `graphdb_encryption_key_version{version="v1|v2|..."}`
- `graphdb_encryption_key_rotations_total`

**Key Management Metrics:**
- `graphdb_key_access_total{key_type="mek|kek|dek", operation="derive|encrypt|decrypt"}`
- `graphdb_key_cache_hits_total`
- `graphdb_key_cache_misses_total`

## References

- NIST SP 800-38D: AES-GCM specification
- NIST SP 800-132: PBKDF2 specification
- FIPS 140-2: Security Requirements for Cryptographic Modules
- OWASP Cryptographic Storage Cheat Sheet
