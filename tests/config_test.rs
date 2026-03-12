use nodefy_agent_lib::config::{save_config, AgentConfig};
use tempfile::TempDir;

#[test]
fn test_default_config() {
    let config = AgentConfig::default();
    assert_eq!(
        config.file_types,
        vec![".csv", ".xlsx", ".xls", ".json", ".xml", ".parquet"]
    );
    assert!(config.recursive);
    assert!(!config.debug);
    assert!(config.bridge_url.is_empty());
    assert!(config.bridge_api_key.is_empty());
}

#[test]
fn test_config_serialization() {
    let config = AgentConfig {
        file_types: vec![".csv".to_string(), ".json".to_string()],
        recursive: false,
        debug: true,
        bridge_url: "ws://localhost:1234".to_string(),
        bridge_api_key: "test-key".to_string(),
    };

    let json = serde_json::to_string(&config).unwrap();
    let deserialized: AgentConfig = serde_json::from_str(&json).unwrap();

    assert_eq!(deserialized.file_types, config.file_types);
    assert_eq!(deserialized.recursive, config.recursive);
    assert_eq!(deserialized.debug, config.debug);
    assert_eq!(deserialized.bridge_url, config.bridge_url);
    assert_eq!(deserialized.bridge_api_key, config.bridge_api_key);
}

#[test]
fn test_save_and_load_config() {
    let dir = TempDir::new().unwrap();
    let config_path = dir.path().join("agent.json");

    let config = AgentConfig {
        file_types: vec![".csv".to_string()],
        recursive: true,
        debug: false,
        bridge_url: "ws://bridge.example.com".to_string(),
        bridge_api_key: String::new(),
    };

    // Save to temp path
    std::fs::create_dir_all(config_path.parent().unwrap()).unwrap();
    let content = serde_json::to_string_pretty(&config).unwrap();
    std::fs::write(&config_path, content).unwrap();

    // Load back
    let loaded: AgentConfig =
        serde_json::from_str(&std::fs::read_to_string(&config_path).unwrap()).unwrap();

    assert_eq!(loaded.file_types, config.file_types);
    assert_eq!(loaded.bridge_url, config.bridge_url);
}

#[test]
fn test_config_partial_deserialization() {
    // Missing fields should use defaults
    let partial_json = r#"{"debug": true}"#;
    let config: AgentConfig = serde_json::from_str(partial_json).unwrap();

    assert!(config.debug);
    assert!(!config.file_types.is_empty()); // should have defaults
    assert!(config.recursive); // default
}
