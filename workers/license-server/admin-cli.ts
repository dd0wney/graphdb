#!/usr/bin/env tsx
/**
 * GraphDB License Server - Admin CLI
 *
 * Manage licenses from the command line
 *
 * Usage:
 *   pnpm tsx admin-cli.ts create <email> <tier> [--expires=2025-12-31]
 *   pnpm tsx admin-cli.ts get <email>
 *   pnpm tsx admin-cli.ts update <email> --tier=enterprise
 *   pnpm tsx admin-cli.ts delete <email>
 *   pnpm tsx admin-cli.ts test <license-key>
 *
 * Environment variables:
 *   LICENSE_SERVER_URL - License server URL (default: http://localhost:8787)
 *   ADMIN_API_KEY - Admin API key for authentication
 */

const API_URL = process.env.LICENSE_SERVER_URL || 'http://localhost:8787';
const API_KEY = process.env.ADMIN_API_KEY;

if (!API_KEY) {
  console.error('Error: ADMIN_API_KEY environment variable not set');
  process.exit(1);
}

interface LicenseData {
  key: string;
  tier: string;
  status: string;
  email: string;
  name?: string;
  issuedAt: string;
  expiresAt: string | null;
  maxNodes?: number | null;
  validationCount?: number;
  lastValidated?: string;
}

async function request(path: string, options: RequestInit = {}): Promise<any> {
  const response = await fetch(`${API_URL}${path}`, {
    ...options,
    headers: {
      Authorization: `Bearer ${API_KEY}`,
      'Content-Type': 'application/json',
      ...options.headers,
    },
  });

  const data = await response.json();

  if (!response.ok) {
    throw new Error(data.error || `HTTP ${response.status}`);
  }

  return data;
}

async function createLicense(args: string[]): Promise<void> {
  const email = args[0];
  const tier = args[1];

  if (!email || !tier) {
    console.error('Usage: create <email> <tier> [--expires=YYYY-MM-DD] [--name=John] [--max-nodes=1000]');
    process.exit(1);
  }

  // Parse optional flags
  const flags = args.slice(2).reduce((acc, arg) => {
    const [key, value] = arg.replace('--', '').split('=');
    acc[key] = value;
    return acc;
  }, {} as Record<string, string>);

  const body: any = { email, tier };

  if (flags.expires) {
    body.expiresAt = flags.expires;
  }

  if (flags.name) {
    body.name = flags.name;
  }

  if (flags['max-nodes']) {
    body.maxNodes = parseInt(flags['max-nodes'], 10);
  }

  console.log('Creating license...');
  const license = await request('/admin/licenses', {
    method: 'POST',
    body: JSON.stringify(body),
  });

  console.log('\n✓ License created successfully!\n');
  console.log('License Key:');
  console.log(license.key);
  console.log('\nLicense Details:');
  console.log(JSON.stringify(license, null, 2));
}

async function getLicense(args: string[]): Promise<void> {
  const email = args[0];

  if (!email) {
    console.error('Usage: get <email>');
    process.exit(1);
  }

  console.log(`Getting license for ${email}...`);
  const license = await request(`/admin/licenses/${encodeURIComponent(email)}`);

  console.log('\nLicense Details:');
  console.log(JSON.stringify(license, null, 2));

  console.log('\nLicense Key:');
  console.log(license.key);
}

async function updateLicense(args: string[]): Promise<void> {
  const email = args[0];

  if (!email) {
    console.error('Usage: update <email> [--tier=pro] [--status=suspended] [--expires=YYYY-MM-DD]');
    process.exit(1);
  }

  // Parse flags
  const flags = args.slice(1).reduce((acc, arg) => {
    const [key, value] = arg.replace('--', '').split('=');
    acc[key] = value;
    return acc;
  }, {} as Record<string, string>);

  const body: any = {};

  if (flags.tier) {
    body.tier = flags.tier;
  }

  if (flags.status) {
    body.status = flags.status;
  }

  if (flags.expires) {
    body.expiresAt = flags.expires;
  }

  if (flags['max-nodes']) {
    body.maxNodes = parseInt(flags['max-nodes'], 10);
  }

  if (Object.keys(body).length === 0) {
    console.error('No updates specified');
    process.exit(1);
  }

  console.log(`Updating license for ${email}...`);
  const license = await request(`/admin/licenses/${encodeURIComponent(email)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  });

  console.log('\n✓ License updated successfully!\n');
  console.log(JSON.stringify(license, null, 2));
}

async function deleteLicense(args: string[]): Promise<void> {
  const email = args[0];

  if (!email) {
    console.error('Usage: delete <email>');
    process.exit(1);
  }

  console.log(`Deleting license for ${email}...`);
  await request(`/admin/licenses/${encodeURIComponent(email)}`, {
    method: 'DELETE',
  });

  console.log('✓ License deleted successfully');
}

async function testLicense(args: string[]): Promise<void> {
  const licenseKey = args[0];

  if (!licenseKey) {
    console.error('Usage: test <license-key>');
    process.exit(1);
  }

  console.log('Testing license key...');

  // Remove authentication for public validate endpoint
  const response = await fetch(`${API_URL}/validate`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ licenseKey }),
  });

  const result = await response.json();

  console.log('\nValidation Result:');
  console.log(JSON.stringify(result, null, 2));

  if (result.valid) {
    console.log('\n✓ License is VALID');
    console.log(`  Tier: ${result.tier}`);
    console.log(`  Status: ${result.status}`);
    if (result.expiresAt) {
      console.log(`  Expires: ${result.expiresAt}`);
    } else {
      console.log(`  Expires: Never`);
    }
  } else {
    console.log('\n✗ License is INVALID');
    console.log(`  Error: ${result.error}`);
  }
}

async function main() {
  const command = process.argv[2];
  const args = process.argv.slice(3);

  try {
    switch (command) {
      case 'create':
        await createLicense(args);
        break;

      case 'get':
        await getLicense(args);
        break;

      case 'update':
        await updateLicense(args);
        break;

      case 'delete':
        await deleteLicense(args);
        break;

      case 'test':
        await testLicense(args);
        break;

      default:
        console.log('GraphDB License Server - Admin CLI');
        console.log('');
        console.log('Usage:');
        console.log('  pnpm tsx admin-cli.ts create <email> <tier> [--expires=YYYY-MM-DD] [--name=Name] [--max-nodes=1000]');
        console.log('  pnpm tsx admin-cli.ts get <email>');
        console.log('  pnpm tsx admin-cli.ts update <email> [--tier=enterprise] [--status=suspended]');
        console.log('  pnpm tsx admin-cli.ts delete <email>');
        console.log('  pnpm tsx admin-cli.ts test <license-key>');
        console.log('');
        console.log('Environment variables:');
        console.log('  LICENSE_SERVER_URL - License server URL (default: http://localhost:8787)');
        console.log('  ADMIN_API_KEY - Admin API key for authentication');
        console.log('');
        console.log('Examples:');
        console.log('  pnpm tsx admin-cli.ts create user@example.com pro');
        console.log('  pnpm tsx admin-cli.ts create user@example.com enterprise --expires=2025-12-31');
        console.log('  pnpm tsx admin-cli.ts get user@example.com');
        console.log('  pnpm tsx admin-cli.ts update user@example.com --tier=enterprise');
        console.log('  pnpm tsx admin-cli.ts test eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...');
        process.exit(1);
    }
  } catch (error) {
    console.error('\nError:', (error as Error).message);
    process.exit(1);
  }
}

main();
