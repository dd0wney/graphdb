import type { LicenseJWTPayload } from './types';

/**
 * Simple JWT implementation using Web Crypto API
 * (Cloudflare Workers compatible)
 */

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function base64UrlEncode(data: ArrayBuffer): string {
  const base64 = btoa(String.fromCharCode(...new Uint8Array(data)));
  return base64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
}

function base64UrlDecode(data: string): ArrayBuffer {
  const base64 = data.replace(/-/g, '+').replace(/_/g, '/');
  const padding = '='.repeat((4 - (base64.length % 4)) % 4);
  const binary = atob(base64 + padding);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

async function importKey(secret: string): Promise<CryptoKey> {
  const keyData = encoder.encode(secret);
  return await crypto.subtle.importKey(
    'raw',
    keyData,
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign', 'verify']
  );
}

/**
 * Sign a JWT with HS256
 */
export async function signJWT(
  payload: LicenseJWTPayload,
  secret: string
): Promise<string> {
  const header = { alg: 'HS256', typ: 'JWT' };

  const encodedHeader = base64UrlEncode(encoder.encode(JSON.stringify(header)));
  const encodedPayload = base64UrlEncode(
    encoder.encode(JSON.stringify(payload))
  );

  const message = `${encodedHeader}.${encodedPayload}`;
  const key = await importKey(secret);
  const signature = await crypto.subtle.sign(
    'HMAC',
    key,
    encoder.encode(message)
  );

  const encodedSignature = base64UrlEncode(signature);

  return `${message}.${encodedSignature}`;
}

/**
 * Verify and decode a JWT
 */
export async function verifyJWT(
  token: string,
  secret: string
): Promise<LicenseJWTPayload | null> {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) {
      return null;
    }

    const [encodedHeader, encodedPayload, encodedSignature] = parts;
    const message = `${encodedHeader}.${encodedPayload}`;

    // Verify signature
    const key = await importKey(secret);
    const signature = base64UrlDecode(encodedSignature);
    const isValid = await crypto.subtle.verify(
      'HMAC',
      key,
      signature,
      encoder.encode(message)
    );

    if (!isValid) {
      return null;
    }

    // Decode payload
    const payload = JSON.parse(
      decoder.decode(base64UrlDecode(encodedPayload))
    ) as LicenseJWTPayload;

    // Check expiration
    if (payload.exp && payload.exp < Math.floor(Date.now() / 1000)) {
      return null;
    }

    return payload;
  } catch (error) {
    console.error('JWT verification error:', error);
    return null;
  }
}

/**
 * Generate a license key (JWT) for a customer
 */
export async function generateLicenseKey(
  licenseId: string,
  email: string,
  tier: string,
  expiresAt: string | null,
  secret: string
): Promise<string> {
  const now = Math.floor(Date.now() / 1000);

  const payload: LicenseJWTPayload = {
    sub: licenseId,
    tier: tier as any,
    email,
    iat: now,
    iss: 'graphdb-license-server',
  };

  // Add expiration if provided
  if (expiresAt) {
    payload.exp = Math.floor(new Date(expiresAt).getTime() / 1000);
  }

  return await signJWT(payload, secret);
}
