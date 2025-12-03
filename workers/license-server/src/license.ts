import type {
  LicenseData,
  LicenseStatus,
  LicenseTier,
  ValidateLicenseResponse,
  CreateLicenseRequest,
  UpdateLicenseRequest,
  Env,
} from './types';
import { verifyJWT, generateLicenseKey } from './jwt';

/**
 * License service for managing licenses in KV
 */
export class LicenseService {
  constructor(private env: Env) {}

  /**
   * Validate a license key
   */
  async validate(
    licenseKey: string,
    instanceId?: string
  ): Promise<ValidateLicenseResponse> {
    try {
      // 1. Verify JWT signature and decode
      const payload = await verifyJWT(licenseKey, this.env.JWT_SECRET);

      if (!payload) {
        return {
          valid: false,
          error: 'Invalid license key signature',
          timestamp: new Date().toISOString(),
        };
      }

      // 2. Look up license in KV
      const licenseData = await this.env.LICENSES.get<LicenseData>(
        `license:${payload.sub}`,
        'json'
      );

      if (!licenseData) {
        return {
          valid: false,
          error: 'License not found',
          timestamp: new Date().toISOString(),
        };
      }

      // 3. Check license status
      if (licenseData.status !== 'active') {
        return {
          valid: false,
          error: `License is ${licenseData.status}`,
          timestamp: new Date().toISOString(),
        };
      }

      // 4. Check expiration
      if (licenseData.expiresAt) {
        const expiresAt = new Date(licenseData.expiresAt);
        if (expiresAt < new Date()) {
          // Update status to expired
          await this.updateStatus(payload.sub, 'expired');

          return {
            valid: false,
            error: 'License expired',
            timestamp: new Date().toISOString(),
          };
        }
      }

      // 5. Update validation metrics (async, don't wait)
      this.updateValidationMetrics(payload.sub, instanceId).catch((err) =>
        console.error('Failed to update metrics:', err)
      );

      // 6. Return success
      return {
        valid: true,
        tier: licenseData.tier,
        status: licenseData.status,
        expiresAt: licenseData.expiresAt,
        maxNodes: licenseData.maxNodes,
        timestamp: new Date().toISOString(),
      };
    } catch (error) {
      console.error('License validation error:', error);
      return {
        valid: false,
        error: 'Internal validation error',
        timestamp: new Date().toISOString(),
      };
    }
  }

  /**
   * Create a new license
   */
  async create(request: CreateLicenseRequest): Promise<LicenseData> {
    const licenseId = crypto.randomUUID();
    const now = new Date().toISOString();

    // Generate license key (JWT)
    const licenseKey = await generateLicenseKey(
      licenseId,
      request.email,
      request.tier,
      request.expiresAt || null,
      this.env.JWT_SECRET
    );

    const licenseData: LicenseData = {
      key: licenseKey,
      tier: request.tier,
      status: 'active',
      email: request.email,
      name: request.name,
      issuedAt: now,
      expiresAt: request.expiresAt || null,
      maxNodes: request.maxNodes,
      metadata: request.metadata,
      validationCount: 0,
    };

    // Store in KV
    await this.env.LICENSES.put(
      `license:${licenseId}`,
      JSON.stringify(licenseData)
    );

    // Also store by email for lookup
    await this.env.LICENSES.put(
      `email:${request.email}`,
      JSON.stringify({ licenseId, tier: request.tier })
    );

    return licenseData;
  }

  /**
   * Get license by ID
   */
  async get(licenseId: string): Promise<LicenseData | null> {
    return await this.env.LICENSES.get<LicenseData>(
      `license:${licenseId}`,
      'json'
    );
  }

  /**
   * Get license by email
   */
  async getByEmail(email: string): Promise<LicenseData | null> {
    const lookup = await this.env.LICENSES.get<{ licenseId: string }>(
      `email:${email}`,
      'json'
    );

    if (!lookup) {
      return null;
    }

    return await this.get(lookup.licenseId);
  }

  /**
   * Update license
   */
  async update(
    licenseId: string,
    updates: UpdateLicenseRequest
  ): Promise<LicenseData | null> {
    const existing = await this.get(licenseId);

    if (!existing) {
      return null;
    }

    const updated: LicenseData = {
      ...existing,
      ...updates,
    };

    // If tier changed, regenerate license key
    if (updates.tier && updates.tier !== existing.tier) {
      updated.key = await generateLicenseKey(
        licenseId,
        existing.email,
        updates.tier,
        updated.expiresAt,
        this.env.JWT_SECRET
      );
    }

    await this.env.LICENSES.put(
      `license:${licenseId}`,
      JSON.stringify(updated)
    );

    return updated;
  }

  /**
   * Update license status
   */
  private async updateStatus(
    licenseId: string,
    status: LicenseStatus
  ): Promise<void> {
    const existing = await this.get(licenseId);

    if (!existing) {
      return;
    }

    existing.status = status;

    await this.env.LICENSES.put(
      `license:${licenseId}`,
      JSON.stringify(existing)
    );
  }

  /**
   * Update validation metrics (async, best-effort)
   */
  private async updateValidationMetrics(
    licenseId: string,
    instanceId?: string
  ): Promise<void> {
    const existing = await this.get(licenseId);

    if (!existing) {
      return;
    }

    existing.lastValidated = new Date().toISOString();
    existing.validationCount = (existing.validationCount || 0) + 1;

    // Store instance ID if provided (for tracking)
    if (instanceId) {
      if (!existing.metadata) {
        existing.metadata = {};
      }
      existing.metadata.lastInstanceId = instanceId;
    }

    await this.env.LICENSES.put(
      `license:${licenseId}`,
      JSON.stringify(existing)
    );
  }

  /**
   * Delete a license
   */
  async delete(licenseId: string): Promise<boolean> {
    const existing = await this.get(licenseId);

    if (!existing) {
      return false;
    }

    // Delete license
    await this.env.LICENSES.delete(`license:${licenseId}`);

    // Delete email lookup
    await this.env.LICENSES.delete(`email:${existing.email}`);

    return true;
  }
}
