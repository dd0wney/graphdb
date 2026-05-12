import requests
import json

class GraphDBClient:
    def __init__(self, base_url="http://localhost:8080", tenant_id="default"):
        self.base_url = base_url
        self.headers = {
            "Content-Type": "application/json",
            "X-Tenant-ID": tenant_id
        }

    def query(self, cypher):
        payload = {"query": cypher}
        response = requests.post(f"{self.base_url}/query", 
                                 data=json.dumps(payload), 
                                 headers=self.headers)
        response.raise_for_status()
        return response.json()

    def vector_search(self, property_name, query_vector, k=5):
        payload = {
            "property_name": property_name,
            "query_vector": query_vector,
            "k": k
        }
        response = requests.post(f"{self.base_url}/vector-search", 
                                 data=json.dumps(payload), 
                                 headers=self.headers)
        response.raise_for_status()
        return response.json()

if __name__ == "__main__":
    client = GraphDBClient()
    
    # Create nodes
    print("Creating nodes...")
    client.query("CREATE (p:Person {name: 'Alice', age: 30})")
    
    # Query nodes
    print("Querying nodes...")
    result = client.query("MATCH (p:Person) RETURN p.name, p.age")
    print(json.dumps(result, indent=2))
