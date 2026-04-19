package com.tsproxy.android.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import tsproxy.Tsproxy
import com.tsproxy.android.service.TsProxyService
import com.tsproxy.android.TsProxyApp
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Application.dataStore by preferencesDataStore("tsproxy_prefs")

data class UiState(
    val running: Boolean = false,
    val socksAddr: String = "127.0.0.1:1080",
    val hostname: String = "ts-proxy-android",
    val tsnetDir: String = "",
    val tailscaleIP: String = "",
    val loginUrl: String = "",
    val logs: String = "",
    val crashLog: String = "",
    val statusText: String = "Stopped"
)

class MainViewModel(app: Application) : AndroidViewModel(app) {

    private val _ui = MutableStateFlow(UiState())
    val ui: StateFlow<UiState> = _ui.asStateFlow()

    init {
        loadConfig()
        startStatusPolling()
    }

    private fun loadConfig() {
        viewModelScope.launch {
            val prefs = getApplication<Application>().dataStore.data.first()
            _ui.value = _ui.value.copy(
                socksAddr = prefs[SOCKS_KEY] ?: "127.0.0.1:1080",
                hostname = prefs[HOSTNAME_KEY] ?: "ts-proxy-android",
                tsnetDir = prefs[TSNETDIR_KEY] ?: ""
            )
        }
    }

    fun saveConfig(socks: String, hostname: String, tsnetDir: String) {
        viewModelScope.launch {
            getApplication<Application>().dataStore.edit { prefs ->
                prefs[SOCKS_KEY] = socks
                prefs[HOSTNAME_KEY] = hostname
                prefs[TSNETDIR_KEY] = tsnetDir
            }
            _ui.value = _ui.value.copy(
                socksAddr = socks,
                hostname = hostname,
                tsnetDir = tsnetDir
            )
        }
    }

    fun startProxy() {
        val ctx = getApplication<Application>()
        val state = _ui.value
        _ui.value = _ui.value.copy(statusText = "Starting...")
        TsProxyApp.appendLog("UI: startProxy clicked, socks=${state.socksAddr} host=${state.hostname}")
        try {
            TsProxyService.start(ctx, state.socksAddr, state.hostname, state.tsnetDir)
        } catch (e: Exception) {
            val sw = java.io.StringWriter()
            e.printStackTrace(java.io.PrintWriter(sw))
            TsProxyApp.appendLog("UI startProxy EXCEPTION: $sw")
            _ui.value = _ui.value.copy(
                statusText = "Failed: ${e.message?.take(100) ?: "unknown"}",
                crashLog = TsProxyApp.readCrashLog()
            )
        }
    }

    fun stopProxy() {
        val ctx = getApplication<Application>()
        _ui.value = _ui.value.copy(statusText = "Stopping...")
        TsProxyApp.appendLog("UI: stopProxy clicked")
        try {
            TsProxyService.stop(ctx)
        } catch (e: Exception) {
            TsProxyApp.appendLog("UI stopProxy EXCEPTION: ${e.message}")
        }
    }

    fun clearCrashLog() {
        TsProxyApp.clearCrashLog()
        _ui.value = _ui.value.copy(crashLog = "")
    }

    fun clearLogs() {
        viewModelScope.launch {
            withContext(Dispatchers.IO) {
                Tsproxy.clearLogs()
            }
            _ui.value = _ui.value.copy(logs = "")
        }
    }

    private fun startStatusPolling() {
        viewModelScope.launch {
            while (true) {
                withContext(Dispatchers.IO) {
                    val running = Tsproxy.isRunning()
                    val ip = if (running) Tsproxy.getTailscaleIP() ?: "" else ""
                    val logs = Tsproxy.getLogs() ?: ""
                    val crashLog = TsProxyApp.readCrashLog()
                    val url = Tsproxy.getLoginURL() ?: ""
                    val newStatus = when {
                        running -> "Running"
                        url.isNotEmpty() -> "需要授权"
                        else -> _ui.value.statusText
                    }
                    _ui.value = _ui.value.copy(
                        running = running,
                        tailscaleIP = ip,
                        logs = logs,
                        loginUrl = url,
                        crashLog = crashLog,
                        statusText = newStatus
                    )
                }
                kotlinx.coroutines.delay(2000)
            }
        }
    }

    companion object {
        private val SOCKS_KEY = stringPreferencesKey("socks_addr")
        private val HOSTNAME_KEY = stringPreferencesKey("hostname")
        private val TSNETDIR_KEY = stringPreferencesKey("tsnet_dir")
    }
}
