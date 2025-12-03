// API Client for GraphDB Admin Dashboard

import type {
	LoginRequest,
	LoginResponse,
	User,
	UserRole,
	CreateUserRequest,
	UpdateUserRequest,
	ListUsersResponse,
	UserResponse,
	APIKey,
	CreateAPIKeyRequest,
	CreateAPIKeyResponse,
	HealthResponse,
	MetricsResponse,
	SecurityHealth,
	AuditLogsResponse,
	KeyInfo,
	QueryRequest,
	QueryResponse,
	NodeResponse,
	EdgeResponse,
	ErrorResponse,
	CreateNodeRequest,
	UpdateNodeRequest,
	CreateEdgeRequest,
	UpdateEdgeRequest
} from './types';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

class APIClient {
	private accessToken: string | null = null;
	private refreshToken: string | null = null;

	constructor() {
		// Load tokens from localStorage if available
		if (typeof window !== 'undefined') {
			this.accessToken = localStorage.getItem('access_token');
			this.refreshToken = localStorage.getItem('refresh_token');
		}
	}

	private async request<T>(
		endpoint: string,
		options: RequestInit = {}
	): Promise<T> {
		const headers: Record<string, string> = {
			'Content-Type': 'application/json',
			...(options.headers as Record<string, string>)
		};

		if (this.accessToken) {
			headers['Authorization'] = `Bearer ${this.accessToken}`;
		}

		const response = await fetch(`${API_BASE}${endpoint}`, {
			...options,
			headers
		});

		if (!response.ok) {
			const error: ErrorResponse = await response.json().catch(() => ({
				error: 'Unknown error',
				message: response.statusText
			}));

			if (response.status === 401 && this.refreshToken) {
				// Try to refresh token
				const refreshed = await this.refreshAccessToken();
				if (refreshed) {
					// Retry the request
					headers['Authorization'] = `Bearer ${this.accessToken}`;
					const retryResponse = await fetch(`${API_BASE}${endpoint}`, {
						...options,
						headers
					});
					if (retryResponse.ok) {
						return retryResponse.json();
					}
				}
			}

			throw new Error(error.message || error.error);
		}

		return response.json();
	}

	// Auth methods
	async login(credentials: LoginRequest): Promise<LoginResponse> {
		const response = await this.request<LoginResponse>('/auth/login', {
			method: 'POST',
			body: JSON.stringify(credentials)
		});

		this.accessToken = response.access_token;
		this.refreshToken = response.refresh_token;

		if (typeof window !== 'undefined') {
			localStorage.setItem('access_token', response.access_token);
			localStorage.setItem('refresh_token', response.refresh_token);
		}

		return response;
	}

	logout(): void {
		this.accessToken = null;
		this.refreshToken = null;
		if (typeof window !== 'undefined') {
			localStorage.removeItem('access_token');
			localStorage.removeItem('refresh_token');
		}
	}

	private async refreshAccessToken(): Promise<boolean> {
		try {
			const response = await fetch(`${API_BASE}/auth/refresh`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ refresh_token: this.refreshToken })
			});

			if (response.ok) {
				const data = await response.json();
				this.accessToken = data.access_token;
				if (typeof window !== 'undefined') {
					localStorage.setItem('access_token', data.access_token);
				}
				return true;
			}
		} catch {
			// Refresh failed
		}
		return false;
	}

	async getCurrentUser(): Promise<User> {
		const response = await this.request<{ user: User }>('/auth/me');
		return response.user;
	}

	isAuthenticated(): boolean {
		return !!this.accessToken;
	}

	// User management (admin only)
	async listUsers(): Promise<User[]> {
		const response = await this.request<ListUsersResponse>('/api/users');
		return response.users;
	}

	async getUser(userId: string): Promise<User> {
		const response = await this.request<UserResponse>(`/api/users/${userId}`);
		return response.user;
	}

	async createUser(req: CreateUserRequest): Promise<User> {
		const response = await this.request<UserResponse>('/api/users', {
			method: 'POST',
			body: JSON.stringify(req)
		});
		return response.user;
	}

	async updateUser(userId: string, role: UserRole): Promise<User> {
		const response = await this.request<UserResponse>(`/api/users/${userId}`, {
			method: 'PUT',
			body: JSON.stringify({ role })
		});
		return response.user;
	}

	async deleteUser(userId: string): Promise<void> {
		await this.request<{ success: boolean }>(`/api/users/${userId}`, {
			method: 'DELETE'
		});
	}

	async changeUserPassword(userId: string, newPassword: string): Promise<void> {
		await this.request<{ success: boolean }>(`/api/users/${userId}/password`, {
			method: 'PUT',
			body: JSON.stringify({ new_password: newPassword })
		});
	}

	// Health endpoints
	async getHealth(): Promise<HealthResponse> {
		return this.request<HealthResponse>('/health');
	}

	// Metrics endpoint (JSON)
	async getMetrics(): Promise<MetricsResponse> {
		return this.request<MetricsResponse>('/api/metrics');
	}

	async getSecurityHealth(): Promise<SecurityHealth> {
		return this.request<SecurityHealth>('/api/v1/security/health');
	}

	// API Key management
	async listAPIKeys(): Promise<{ keys: APIKey[] }> {
		return this.request<{ keys: APIKey[] }>('/api/v1/apikeys');
	}

	async createAPIKey(req: CreateAPIKeyRequest): Promise<CreateAPIKeyResponse> {
		return this.request<CreateAPIKeyResponse>('/api/v1/apikeys', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async revokeAPIKey(keyId: string): Promise<{ message: string }> {
		return this.request<{ message: string }>(`/api/v1/apikeys/${keyId}`, {
			method: 'DELETE'
		});
	}

	// Audit logs
	async getAuditLogs(params?: {
		user_id?: string;
		username?: string;
		action?: string;
		resource_type?: string;
		status?: string;
		start_time?: string;
		end_time?: string;
		limit?: number;
	}): Promise<AuditLogsResponse> {
		const query = new URLSearchParams();
		if (params) {
			Object.entries(params).forEach(([key, value]) => {
				if (value !== undefined) {
					query.set(key, String(value));
				}
			});
		}
		const queryString = query.toString();
		return this.request<AuditLogsResponse>(
			`/api/v1/security/audit/logs${queryString ? `?${queryString}` : ''}`
		);
	}

	async exportAuditLogs(): Promise<Blob> {
		const response = await fetch(`${API_BASE}/api/v1/security/audit/export`, {
			method: 'POST',
			headers: {
				Authorization: `Bearer ${this.accessToken}`
			}
		});
		return response.blob();
	}

	// Encryption key management
	async getKeyInfo(): Promise<KeyInfo> {
		return this.request<KeyInfo>('/api/v1/security/keys/info');
	}

	async rotateKey(): Promise<{ message: string; new_version: number; timestamp: string }> {
		return this.request<{ message: string; new_version: number; timestamp: string }>(
			'/api/v1/security/keys/rotate',
			{ method: 'POST' }
		);
	}

	// Database operations
	async query(req: QueryRequest): Promise<QueryResponse> {
		return this.request<QueryResponse>('/query', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async getNodes(limit = 100): Promise<NodeResponse[]> {
		return this.request<NodeResponse[]>(`/nodes?limit=${limit}`);
	}

	async getNode(id: number): Promise<NodeResponse> {
		return this.request<NodeResponse>(`/nodes/${id}`);
	}

	async getEdges(limit = 100): Promise<EdgeResponse[]> {
		return this.request<EdgeResponse[]>(`/edges?limit=${limit}`);
	}

	async getEdge(id: number): Promise<EdgeResponse> {
		return this.request<EdgeResponse>(`/edges/${id}`);
	}

	// Node CRUD operations
	async createNode(req: CreateNodeRequest): Promise<NodeResponse> {
		return this.request<NodeResponse>('/nodes', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async updateNode(id: number, req: UpdateNodeRequest): Promise<NodeResponse> {
		return this.request<NodeResponse>(`/nodes/${id}`, {
			method: 'PUT',
			body: JSON.stringify(req)
		});
	}

	async deleteNode(id: number): Promise<{ success: boolean }> {
		return this.request<{ success: boolean }>(`/nodes/${id}`, {
			method: 'DELETE'
		});
	}

	// Edge CRUD operations
	async createEdge(req: CreateEdgeRequest): Promise<EdgeResponse> {
		return this.request<EdgeResponse>('/edges', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	async updateEdge(id: number, req: UpdateEdgeRequest): Promise<EdgeResponse> {
		return this.request<EdgeResponse>(`/edges/${id}`, {
			method: 'PUT',
			body: JSON.stringify(req)
		});
	}

	async deleteEdge(id: number): Promise<{ success: boolean }> {
		return this.request<{ success: boolean }>(`/edges/${id}`, {
			method: 'DELETE'
		});
	}
}

export const api = new APIClient();
export default api;
