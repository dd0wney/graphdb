/**
 * License tiers for GraphDB
 */
export type LicenseTier = 'community' | 'pro' | 'enterprise';

/**
 * License status
 */
export type LicenseStatus = 'active' | 'suspended' | 'cancelled' | 'expired';

/**
 * License metadata stored in KV
 */
export interface LicenseData {
  /** Unique license key (JWT) */
  key: string;

  /** License tier */
  tier: LicenseTier;

  /** License status */
  status: LicenseStatus;

  /** Customer email */
  email: string;

  /** Customer name (optional) */
  name?: string;

  /** Stripe customer ID */
  stripeCustomerId?: string;

  /** Stripe subscription ID */
  stripeSubscriptionId?: string;

  /** License issue date (ISO 8601) */
  issuedAt: string;

  /** License expiration date (ISO 8601, null for lifetime) */
  expiresAt: string | null;

  /** Maximum number of nodes this license supports (null for unlimited) */
  maxNodes?: number | null;

  /** Custom metadata */
  metadata?: Record<string, unknown>;

  /** Last validation timestamp */
  lastValidated?: string;

  /** Number of times this license has been validated */
  validationCount?: number;
}

/**
 * JWT payload for license keys
 */
export interface LicenseJWTPayload {
  /** License ID (used to look up in KV) */
  sub: string;

  /** License tier */
  tier: LicenseTier;

  /** Customer email */
  email: string;

  /** Issued at (Unix timestamp) */
  iat: number;

  /** Expires at (Unix timestamp, null for lifetime) */
  exp?: number;

  /** Issuer */
  iss: string;
}

/**
 * License validation request
 */
export interface ValidateLicenseRequest {
  /** License key to validate */
  licenseKey: string;

  /** Instance ID (optional, for tracking) */
  instanceId?: string;

  /** GraphDB version (optional, for compatibility checking) */
  version?: string;
}

/**
 * License validation response
 */
export interface ValidateLicenseResponse {
  /** Whether the license is valid */
  valid: boolean;

  /** License tier (if valid) */
  tier?: LicenseTier;

  /** License status (if valid) */
  status?: LicenseStatus;

  /** Error message (if invalid) */
  error?: string;

  /** License expiration date (ISO 8601, if applicable) */
  expiresAt?: string | null;

  /** Maximum nodes allowed (if applicable) */
  maxNodes?: number | null;

  /** Server timestamp */
  timestamp: string;
}

/**
 * Stripe webhook event types we handle
 */
export type StripeEvent =
  | 'checkout.session.completed'
  | 'customer.subscription.created'
  | 'customer.subscription.updated'
  | 'customer.subscription.deleted'
  | 'invoice.paid'
  | 'invoice.payment_failed';

/**
 * Admin API: Create license request
 */
export interface CreateLicenseRequest {
  email: string;
  name?: string;
  tier: LicenseTier;
  expiresAt?: string | null;
  maxNodes?: number | null;
  metadata?: Record<string, unknown>;
}

/**
 * Admin API: Update license request
 */
export interface UpdateLicenseRequest {
  status?: LicenseStatus;
  tier?: LicenseTier;
  expiresAt?: string | null;
  maxNodes?: number | null;
  metadata?: Record<string, unknown>;
}

/**
 * Cloudflare Workers environment bindings
 */
export interface Env {
  // KV namespace for licenses
  LICENSES: KVNamespace;

  // Secrets
  JWT_SECRET: string;
  STRIPE_SECRET_KEY: string;
  STRIPE_WEBHOOK_SECRET: string;
  ADMIN_API_KEY: string;

  // Environment variables
  ENVIRONMENT: string;
}
