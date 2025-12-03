import type { Env, LicenseTier } from './types';
import { LicenseService } from './license';
import Stripe from 'stripe';

/**
 * Stripe webhook handler
 */
export class StripeWebhookHandler {
  private stripe: Stripe;
  private licenseService: LicenseService;

  constructor(private env: Env) {
    this.stripe = new Stripe(env.STRIPE_SECRET_KEY, {
      apiVersion: '2023-10-16',
      httpClient: Stripe.createFetchHttpClient(),
    });
    this.licenseService = new LicenseService(env);
  }

  /**
   * Verify Stripe webhook signature
   */
  async verifyWebhook(
    body: string,
    signature: string
  ): Promise<Stripe.Event | null> {
    try {
      const event = await this.stripe.webhooks.constructEventAsync(
        body,
        signature,
        this.env.STRIPE_WEBHOOK_SECRET
      );
      return event;
    } catch (error) {
      console.error('Webhook signature verification failed:', error);
      return null;
    }
  }

  /**
   * Handle webhook event
   */
  async handleEvent(event: Stripe.Event): Promise<void> {
    console.log(`Processing webhook: ${event.type}`);

    try {
      switch (event.type) {
        case 'checkout.session.completed':
          await this.handleCheckoutCompleted(
            event.data.object as Stripe.Checkout.Session
          );
          break;

        case 'customer.subscription.created':
        case 'customer.subscription.updated':
          await this.handleSubscriptionUpdated(
            event.data.object as Stripe.Subscription
          );
          break;

        case 'customer.subscription.deleted':
          await this.handleSubscriptionDeleted(
            event.data.object as Stripe.Subscription
          );
          break;

        case 'invoice.payment_failed':
          await this.handlePaymentFailed(
            event.data.object as Stripe.Invoice
          );
          break;

        default:
          console.log(`Unhandled event type: ${event.type}`);
      }
    } catch (error) {
      console.error(`Error handling webhook ${event.type}:`, error);
      throw error;
    }
  }

  /**
   * Handle successful checkout - create new license
   */
  private async handleCheckoutCompleted(
    session: Stripe.Checkout.Session
  ): Promise<void> {
    console.log('Checkout completed:', session.id);

    const customerEmail = session.customer_email || session.customer_details?.email;
    if (!customerEmail) {
      throw new Error('No customer email in checkout session');
    }

    // Get price to determine tier
    const tier = this.getTierFromPriceId(session.line_items?.data[0]?.price?.id);

    // Check if license already exists for this email
    const existing = await this.licenseService.getByEmail(customerEmail);

    if (existing) {
      console.log('License already exists, updating tier');
      await this.licenseService.update(existing.key, { tier });
      return;
    }

    // Create new license
    const license = await this.licenseService.create({
      email: customerEmail,
      name: session.customer_details?.name,
      tier,
      expiresAt: null, // Subscription-based, no expiration
      metadata: {
        stripeCustomerId: session.customer as string,
        stripeSubscriptionId: session.subscription as string,
        checkoutSessionId: session.id,
      },
    });

    console.log('Created license:', license.key);

    // TODO: Send email with license key
    // await this.sendLicenseEmail(customerEmail, license.key);
  }

  /**
   * Handle subscription update
   */
  private async handleSubscriptionUpdated(
    subscription: Stripe.Subscription
  ): Promise<void> {
    console.log('Subscription updated:', subscription.id);

    const customer = await this.stripe.customers.retrieve(
      subscription.customer as string
    );

    if (customer.deleted) {
      throw new Error('Customer deleted');
    }

    const email = customer.email;
    if (!email) {
      throw new Error('No customer email');
    }

    const license = await this.licenseService.getByEmail(email);
    if (!license) {
      console.warn('License not found for subscription update');
      return;
    }

    // Get new tier from subscription
    const tier = this.getTierFromPriceId(subscription.items.data[0]?.price.id);

    // Update license
    await this.licenseService.update(license.key, {
      tier,
      status: subscription.status === 'active' ? 'active' : 'suspended',
      metadata: {
        ...license.metadata,
        stripeSubscriptionId: subscription.id,
        subscriptionStatus: subscription.status,
      },
    });

    console.log('Updated license tier:', tier);
  }

  /**
   * Handle subscription cancellation
   */
  private async handleSubscriptionDeleted(
    subscription: Stripe.Subscription
  ): Promise<void> {
    console.log('Subscription deleted:', subscription.id);

    const customer = await this.stripe.customers.retrieve(
      subscription.customer as string
    );

    if (customer.deleted) {
      return;
    }

    const email = customer.email;
    if (!email) {
      return;
    }

    const license = await this.licenseService.getByEmail(email);
    if (!license) {
      return;
    }

    // Mark license as cancelled
    await this.licenseService.update(license.key, {
      status: 'cancelled',
    });

    console.log('Cancelled license');
  }

  /**
   * Handle payment failure
   */
  private async handlePaymentFailed(invoice: Stripe.Invoice): Promise<void> {
    console.log('Payment failed:', invoice.id);

    const customer = await this.stripe.customers.retrieve(
      invoice.customer as string
    );

    if (customer.deleted) {
      return;
    }

    const email = customer.email;
    if (!email) {
      return;
    }

    const license = await this.licenseService.getByEmail(email);
    if (!license) {
      return;
    }

    // Suspend license after payment failure
    await this.licenseService.update(license.key, {
      status: 'suspended',
    });

    console.log('Suspended license due to payment failure');
  }

  /**
   * Map Stripe price ID to license tier
   */
  private getTierFromPriceId(priceId?: string): LicenseTier {
    if (!priceId) {
      return 'pro'; // Default
    }

    // TODO: Map your actual Stripe price IDs here
    // Example:
    // price_pro_monthly -> pro
    // price_enterprise_monthly -> enterprise

    if (priceId.includes('enterprise')) {
      return 'enterprise';
    }

    return 'pro';
  }
}
