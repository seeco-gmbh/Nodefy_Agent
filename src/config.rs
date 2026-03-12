use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct AgentConfig {
    #[serde(default = "default_file_types")]
    pub file_types: Vec<String>,
    #[serde(default = "default_recursive")]
    pub recursive: bool,
    #[serde(default)]
    pub debug: bool,
    #[serde(default)]
    pub bridge_url: String,
    #[serde(default)]
    pub bridge_api_key: String,
}

fn default_file_types() -> Vec<String> {
    vec![
        ".csv".into(),
        ".xlsx".into(),
        ".xls".into(),
        ".json".into(),
        ".xml".into(),
        ".parquet".into(),
    ]
}

fn default_recursive() -> bool {
    true
}

impl Default for AgentConfig {
    fn default() -> Self {
        Self {
            file_types: default_file_types(),
            recursive: default_recursive(),
            debug: false,
            bridge_url: String::new(),
            bridge_api_key: String::new(),
        }
    }
}

pub fn config_dir() -> PathBuf {
    dirs::home_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join(".nodefy")
}

pub fn config_path() -> PathBuf {
    config_dir().join("agent.json")
}

pub fn load_config() -> AgentConfig {
    let path = config_path();
    if !path.exists() {
        return AgentConfig::default();
    }

    match std::fs::read_to_string(&path) {
        Ok(content) => serde_json::from_str(&content).unwrap_or_default(),
        Err(_) => AgentConfig::default(),
    }
}

pub fn save_config(config: &AgentConfig) -> anyhow::Result<()> {
    let dir = config_dir();
    std::fs::create_dir_all(&dir)?;
    let content = serde_json::to_string_pretty(config)?;
    std::fs::write(config_path(), content)?;
    Ok(())
}
