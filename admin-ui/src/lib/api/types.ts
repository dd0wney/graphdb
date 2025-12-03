// API Response Types for GraphDB Admin Dashboard

export type UserRole = 'admin' | 'editor' | 'viewer';

export interface User {
	id: string;
	username: string;
	role: UserRole;
	created_at: number; // Unix timestamp
}

export interface CreateUserRequest {
	username: string;
	password: string;
	role?: UserRole;
}

export interface UpdateUserRequest {
	role: UserRole;
}

export interface ChangePasswordRequest {
	new_password: string;
}

export interface ListUsersResponse {
	users: User[];
}

export interface UserResponse {
	user: User;
}

export interface APIKey {
	id: string;
	name: string;
	user_id: string;
	prefix: string;
	permissions: string[];
	created_at: string;
	expires_at?: string;
	last_used_at?: string;
	revoked: boolean;
}

export interface CreateAPIKeyRequest {
	name: string;
	permissions: string[];
	expires_in: number; // seconds, 0 = never
}

export interface CreateAPIKeyResponse {
	key: string; // Only returned once
	api_key: APIKey;
}

export interface LoginRequest {
	username: string;
	password: string;
}

export interface LoginResponse {
	access_token: string;
	refresh_token: string;
	expires_in: number;
	user: User;
}

export interface HealthResponse {
	status: string;
	timestamp: string;
	version: string;
	edition: string;
	features: string[];
	uptime: string;
	checks?: Record<string, unknown>;
}

export interface MetricsResponse {
	node_count: number;
	edge_count: number;
	total_queries: number;
	avg_query_time_ms: number;
	memory_used_mb: number;
	memory_total_mb: number;
	num_goroutines: number;
	num_cpu: number;
	uptime: string;
	uptime_seconds: number;
}

export interface SecurityHealth {
	timestamp: string;
	status: string;
	components: {
		encryption: {
			enabled: boolean;
			key_stats?: {
				total_keys: number;
				current_version: number;
				oldest_key_age: string;
			};
		};
		tls: {
			enabled: boolean;
		};
		audit: {
			enabled: boolean;
			event_count: number;
		};
		authentication: {
			jwt_enabled: boolean;
			apikey_enabled: boolean;
		};
	};
}

export interface AuditEvent {
	id: string;
	timestamp: string;
	user_id: string;
	username: string;
	action: string;
	resource_type: string;
	resource_id: string;
	status: 'success' | 'failure';
	ip_address: string;
	user_agent: string;
	details?: Record<string, unknown>;
}

export interface AuditLogsResponse {
	events: AuditEvent[];
	count: number;
	total: number;
	persistent_audit?: {
		enabled: boolean;
		total_persisted: number;
		total_files: number;
		total_size_bytes: number;
		current_file: string;
	};
}

export interface KeyInfo {
	statistics: {
		total_keys: number;
		current_version: number;
		encryptions: number;
		decryptions: number;
	};
	keys: Array<{
		version: number;
		created_at: string;
		algorithm: string;
	}>;
}

export interface ReplicationState {
	node_id: string;
	role: 'primary' | 'replica' | 'standalone';
	current_lsn: number;
	replica_count: number;
	replicas: ReplicaInfo[];
}

export interface ReplicaInfo {
	replica_id: string;
	connected: boolean;
	last_applied_lsn: number;
	last_seen: string;
	heartbeat_lag: number;
}

export interface NodeResponse {
	id: number;
	labels: string[];
	properties: Record<string, unknown>;
}

export interface EdgeResponse {
	id: number;
	from_node_id: number;
	to_node_id: number;
	type: string;
	properties: Record<string, unknown>;
	weight: number;
}

export interface QueryRequest {
	query: string;
	parameters?: Record<string, unknown>;
}

export interface QueryResponse {
	columns: string[];
	rows: Record<string, unknown>[];
	count: number;
	time: string;
}

export interface ErrorResponse {
	error: string;
	message: string;
	code?: number;
}

// Node/Edge CRUD types
export interface CreateNodeRequest {
	labels: string[];
	properties?: Record<string, unknown>;
}

export interface UpdateNodeRequest {
	labels?: string[];
	properties?: Record<string, unknown>;
}

export interface CreateEdgeRequest {
	from_node_id: number;
	to_node_id: number;
	type: string;
	properties?: Record<string, unknown>;
	weight?: number;
}

export interface UpdateEdgeRequest {
	type?: string;
	properties?: Record<string, unknown>;
	weight?: number;
}
