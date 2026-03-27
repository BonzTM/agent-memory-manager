import os
import requests
import json

def main():
    api_url = os.getenv("AMM_API_URL", "http://localhost:8080")
    project_id = os.getenv("CODEX_PROJECT_ID", "unknown")
    
    payload = {
        "kind": "session_end",
        "source_system": "codex",
        "content": f"Codex session ended for project: {project_id}"
    }
    
    try:
        response = requests.post(f"{api_url}/v1/events", json=payload)
        response.raise_for_status()
        print(f"Event ingested successfully: {response.status_code}")
    except Exception as e:
        print(f"Error during ingestion: {e}")

if __name__ == "__main__":
    main()
