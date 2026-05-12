package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GraphDBClient struct {
	BaseURL  string
	TenantID string
}

func NewClient(baseURL, tenantID string) *GraphDBClient {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if tenantID == "" {
		tenantID = "default"
	}
	return &GraphDBClient{BaseURL: baseURL, TenantID: tenantID}
}

func (c *GraphDBClient) Query(cypher string) (map[string]any, error) {
	payload := map[string]string{"query": cypher}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", c.BaseURL+"/query", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", c.TenantID)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

func main() {
	client := NewClient("", "")
	res, err := client.Query("MATCH (n) RETURN n LIMIT 1")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %v\n", res)
}
