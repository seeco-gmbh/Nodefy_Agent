use anyhow::{anyhow, Result};
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{collections::HashMap, sync::Arc, time::Duration};
use tauri::{AppHandle, Emitter};
use tokio::{
    sync::{mpsc, oneshot, Mutex, Notify, RwLock},
    time::{sleep, timeout},
};
use tokio_tungstenite::{connect_async, tungstenite::Message as WsMessage};
use tracing::{debug, error, info, warn};
use uuid::Uuid;

const BRIDGE_EVENT_NAME: &str = "bridge-event";
const MAX_RECONNECT_ATTEMPTS: u32 = 5;
const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BridgeMessage {
    #[serde(rename = "type")]
    pub msg_type: String,
    pub payload: Value,
    #[serde(rename = "requestId", skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BridgeResponse {
    pub method: String,
    pub payload: Value,
    #[serde(rename = "requestId", skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
}

#[derive(Debug)]
enum BridgeCommand {
    Send {
        msg: BridgeMessage,
        reply: Option<oneshot::Sender<Result<Value>>>,
        expected_method: Option<String>,
    },
    Disconnect,
}

type PendingRequestMap =
    Arc<Mutex<HashMap<String, (Option<String>, oneshot::Sender<Result<Value>>)>>>;

#[derive(Debug, Clone, Default)]
pub struct BridgeConnectionState {
    pub connected: bool,
    pub authenticated: bool,
    pub url: String,
}

pub struct BridgeClient {
    state: Arc<RwLock<BridgeConnectionState>>,
    tx: Arc<Mutex<Option<mpsc::Sender<BridgeCommand>>>>,
    connected_notify: Arc<Notify>,
}

impl Default for BridgeClient {
    fn default() -> Self {
        Self::new()
    }
}

impl BridgeClient {
    pub fn new() -> Self {
        Self {
            state: Arc::new(RwLock::new(BridgeConnectionState::default())),
            tx: Arc::new(Mutex::new(None)),
            connected_notify: Arc::new(Notify::new()),
        }
    }

    pub async fn connect(
        &self,
        app: AppHandle,
        url: String,
        api_key: Option<String>,
    ) -> Result<()> {
        {
            let state = self.state.read().await;
            if state.connected {
                return Ok(());
            }
        }

        info!("Connecting to Adapt Bridge: {}", url);

        let (tx, rx) = mpsc::channel::<BridgeCommand>(64);
        {
            let mut lock = self.tx.lock().await;
            *lock = Some(tx);
        }

        let state = self.state.clone();
        let notify = self.connected_notify.clone();
        let url_clone = url.clone();
        let api_key_clone = api_key.clone();

        tokio::spawn(async move {
            bridge_loop(app, state, notify, url_clone, api_key_clone, rx).await;
        });

        // Wait for actual connection confirmation instead of a blind sleep
        match timeout(CONNECT_TIMEOUT, self.connected_notify.notified()).await {
            Ok(_) => {
                let connected = self.state.read().await.connected;
                if connected {
                    Ok(())
                } else {
                    Err(anyhow!("Failed to connect to Adapt Bridge at {}", url))
                }
            }
            Err(_) => Err(anyhow!(
                "Timed out waiting for Adapt Bridge connection at {} ({}s)",
                url,
                CONNECT_TIMEOUT.as_secs()
            )),
        }
    }

    pub async fn disconnect(&self) -> Result<()> {
        let tx = self.tx.lock().await.clone();
        if let Some(tx) = tx {
            let _ = tx.send(BridgeCommand::Disconnect).await;
        }
        let mut state = self.state.write().await;
        state.connected = false;
        state.authenticated = false;
        Ok(())
    }

    pub async fn get_status(&self) -> BridgeConnectionState {
        self.state.read().await.clone()
    }

    pub async fn send_and_wait(
        &self,
        msg_type: &str,
        payload: Value,
        expected_method: &str,
        timeout: Duration,
    ) -> Result<Value> {
        let tx = {
            let lock = self.tx.lock().await;
            lock.clone()
                .ok_or_else(|| anyhow!("Not connected to Adapt Bridge"))?
        };

        let request_id = Uuid::new_v4().to_string();
        let msg = BridgeMessage {
            msg_type: msg_type.to_string(),
            payload,
            request_id: Some(request_id.clone()),
        };

        let (reply_tx, reply_rx) = oneshot::channel();

        tx.send(BridgeCommand::Send {
            msg,
            reply: Some(reply_tx),
            expected_method: Some(expected_method.to_string()),
        })
        .await
        .map_err(|_| anyhow!("Bridge channel closed"))?;

        match tokio::time::timeout(timeout, reply_rx).await {
            Ok(Ok(result)) => result,
            Ok(Err(_)) => Err(anyhow!("Reply channel closed")),
            Err(_) => Err(anyhow!("Timeout waiting for {} response", expected_method)),
        }
    }

    pub async fn send(&self, msg_type: &str, payload: Value) -> Result<()> {
        let tx = {
            let lock = self.tx.lock().await;
            lock.clone()
                .ok_or_else(|| anyhow!("Not connected to Adapt Bridge"))?
        };

        let msg = BridgeMessage {
            msg_type: msg_type.to_string(),
            payload,
            request_id: None,
        };

        tx.send(BridgeCommand::Send {
            msg,
            reply: None,
            expected_method: None,
        })
        .await
        .map_err(|_| anyhow!("Bridge channel closed"))?;

        Ok(())
    }
}

async fn bridge_loop(
    app: AppHandle,
    state: Arc<RwLock<BridgeConnectionState>>,
    connected_notify: Arc<Notify>,
    url: String,
    api_key: Option<String>,
    mut rx: mpsc::Receiver<BridgeCommand>,
) {
    for attempt in 0..=MAX_RECONNECT_ATTEMPTS {
        if attempt > 0 {
            let delay = Duration::from_secs(2u64.pow(attempt - 1).min(16));
            info!(
                "Reconnecting to bridge in {:?} (attempt {}/{})",
                delay, attempt, MAX_RECONNECT_ATTEMPTS
            );
            sleep(delay).await;
        }

        match connect_async(&url).await {
            Ok((ws_stream, _)) => {
                info!("Connected to Adapt Bridge: {}", url);

                {
                    let mut s = state.write().await;
                    s.connected = true;
                    s.url = url.clone();
                }

                // Notify connect() that the connection is up
                connected_notify.notify_one();

                let (mut ws_tx, mut ws_rx) = ws_stream.split();

                // Authenticate if api_key is set
                if let Some(ref key) = api_key {
                    let auth_msg = serde_json::json!({
                        "type": "Authenticate",
                        "payload": { "apiKey": key },
                        "requestId": Uuid::new_v4().to_string()
                    });
                    if let Ok(text) = serde_json::to_string(&auth_msg) {
                        let _ = ws_tx.send(WsMessage::Text(text.into())).await;
                    }
                }

                // Pending request map: request_id -> (expected_method, reply_sender)
                let pending: PendingRequestMap = Arc::new(Mutex::new(HashMap::new()));

                let pending_clone = pending.clone();
                let app_clone = app.clone();
                let state_clone = state.clone();

                // Spawn receiver task
                let recv_task = tokio::spawn(async move {
                    while let Some(msg) = ws_rx.next().await {
                        match msg {
                            Ok(WsMessage::Text(text)) => {
                                match serde_json::from_str::<BridgeResponse>(&text) {
                                    Ok(resp) => {
                                        if resp.method == "Heartbeat" {
                                            continue;
                                        }
                                        if resp.method == "Authenticated" {
                                            let mut s = state_clone.write().await;
                                            s.authenticated = true;
                                            info!("Authenticated with Adapt Bridge");
                                            continue;
                                        }

                                        // Try to resolve pending request
                                        let mut pending = pending_clone.lock().await;
                                        let resolved = if let Some(ref rid) = resp.request_id {
                                            if let Some((_, reply)) = pending.remove(rid) {
                                                if resp.method == "Error" {
                                                    let msg = resp
                                                        .payload
                                                        .get("message")
                                                        .and_then(|m| m.as_str())
                                                        .unwrap_or("Bridge error")
                                                        .to_string();
                                                    let _ = reply.send(Err(anyhow!(msg)));
                                                } else {
                                                    let _ = reply.send(Ok(resp.payload.clone()));
                                                }
                                                true
                                            } else {
                                                false
                                            }
                                        } else {
                                            // No request ID - try to match by expected method
                                            let key = pending
                                                .keys()
                                                .find(|k| {
                                                    pending
                                                        .get(*k)
                                                        .and_then(|(em, _)| em.as_deref())
                                                        .map(|em| em == resp.method)
                                                        .unwrap_or(false)
                                                })
                                                .cloned();

                                            if let Some(k) = key {
                                                if let Some((_, reply)) = pending.remove(&k) {
                                                    let _ = reply.send(Ok(resp.payload.clone()));
                                                }
                                                true
                                            } else {
                                                false
                                            }
                                        };

                                        drop(pending);

                                        // Forward unhandled events to frontend
                                        if !resolved {
                                            let event = crate::events::BridgeEvent {
                                                method: resp.method,
                                                payload: resp.payload,
                                            };
                                            if let Err(e) =
                                                app_clone.emit(BRIDGE_EVENT_NAME, &event)
                                            {
                                                error!("Failed to emit bridge event: {}", e);
                                            }
                                        }
                                    }
                                    Err(e) => {
                                        debug!(
                                            "Failed to parse bridge message: {} - text: {}",
                                            e,
                                            &text[..text.len().min(200)]
                                        );
                                    }
                                }
                            }
                            Ok(WsMessage::Close(_)) | Err(_) => break,
                            _ => {}
                        }
                    }

                    let mut s = state_clone.write().await;
                    s.connected = false;
                    s.authenticated = false;
                    info!("Disconnected from Adapt Bridge");
                });

                // Process send commands until disconnect or channel close
                let mut disconnected = false;
                while let Some(cmd) = rx.recv().await {
                    match cmd {
                        BridgeCommand::Disconnect => {
                            disconnected = true;
                            let _ = ws_tx.send(WsMessage::Close(None)).await;
                            break;
                        }
                        BridgeCommand::Send {
                            msg,
                            reply,
                            expected_method,
                        } => match serde_json::to_string(&msg) {
                            Ok(text) => {
                                if let Err(e) = ws_tx.send(WsMessage::Text(text.into())).await {
                                    error!("Failed to send to bridge: {}", e);
                                    if let Some(reply) = reply {
                                        let _ = reply.send(Err(anyhow!("Send failed: {}", e)));
                                    }
                                    break;
                                }
                                if let (Some(reply), Some(rid)) = (reply, msg.request_id) {
                                    let mut pending = pending.lock().await;
                                    pending.insert(rid, (expected_method, reply));
                                }
                            }
                            Err(e) => {
                                error!("Failed to serialize message: {}", e);
                                if let Some(reply) = reply {
                                    let _ = reply.send(Err(anyhow!("Serialize failed: {}", e)));
                                }
                            }
                        },
                    }
                }

                recv_task.abort();

                if disconnected {
                    info!("Bridge disconnected by user");
                    return;
                }

                // Unintentional disconnect - retry
                warn!("Bridge connection lost, will retry...");
            }
            Err(e) => {
                warn!(
                    "Failed to connect to bridge (attempt {}): {}",
                    attempt + 1,
                    e
                );
            }
        }
    }

    error!("Exhausted bridge reconnection attempts");
    let mut s = state.write().await;
    s.connected = false;
}
