use std::sync::Arc;
use tokio::sync::Mutex;

use crate::{
    config::AgentConfig,
    services::{bridge::BridgeClient, watcher::WatcherService},
};

pub struct AgentState {
    pub config: Arc<Mutex<AgentConfig>>,
    pub watcher: Arc<Mutex<WatcherService>>,
    pub bridge: Arc<BridgeClient>,
}

impl AgentState {
    pub fn new(config: AgentConfig) -> Self {
        let watcher = WatcherService::new(config.file_types.clone(), config.recursive);
        Self {
            config: Arc::new(Mutex::new(config)),
            watcher: Arc::new(Mutex::new(watcher)),
            bridge: Arc::new(BridgeClient::new()),
        }
    }
}
