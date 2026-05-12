import axios from 'axios';

export class GraphDBClient {
  private baseUrl: string;
  private headers: Record<string, string>;

  constructor(baseUrl: string = 'http://localhost:8080', tenantId: string = 'default') {
    this.baseUrl = baseUrl;
    this.headers = {
      'Content-Type': 'application/json',
      'X-Tenant-ID': tenantId,
    };
  }

  async query(cypher: string) {
    const response = await axios.post(`${this.baseUrl}/query`, { query: cypher }, { headers: this.headers });
    return response.data;
  }

  async vectorSearch(propertyName: string, queryVector: number[], k: number = 5) {
    const response = await axios.post(
      `${this.baseUrl}/vector-search`,
      { property_name: propertyName, query_vector: queryVector, k },
      { headers: this.headers }
    );
    return response.data;
  }
}

// Example usage:
// const client = new GraphDBClient();
// client.query('MATCH (n) RETURN n').then(console.log);
