package com.tsproxy.android.service

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import androidx.core.app.NotificationCompat
import com.tsproxy.android.R
import com.tsproxy.android.TsProxyApp
import com.tsproxy.android.ui.MainActivity
import tsproxy.Tsproxy

class TsProxyService : Service() {

    private var wakeLock: PowerManager.WakeLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> {
                val socks = intent.getStringExtra(EXTRA_SOCKS) ?: "127.0.0.1:1080"
                val hostname = intent.getStringExtra(EXTRA_HOSTNAME) ?: "ts-socks5"
                val tsnetDir = intent.getStringExtra(EXTRA_TSNET_DIR) ?: ""
                startProxy(socks, hostname, tsnetDir)
            }
            ACTION_STOP -> {
                stopProxy()
            }
        }
        return START_STICKY
    }

    private fun acquireWakeLock() {
        try {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = pm.newWakeLock(
                PowerManager.PARTIAL_WAKE_LOCK,
                "ts-proxy::keepalive"
            )
            wakeLock?.acquire(24 * 60 * 60 * 1000L) // 24 hours
            TsProxyApp.appendLog("WakeLock acquired")
        } catch (e: Exception) {
            TsProxyApp.appendLog("WakeLock failed: ${e.message}")
        }
    }

    private fun releaseWakeLock() {
        try {
            wakeLock?.let {
                if (it.isHeld) {
                    it.release()
                    TsProxyApp.appendLog("WakeLock released")
                }
            }
            wakeLock = null
        } catch (e: Exception) {
            TsProxyApp.appendLog("WakeLock release error: ${e.message}")
        }
    }

    private fun startProxy(socks: String, hostname: String, tsnetDir: String) {
        val notification = buildNotification("Starting ts-socks5...")
        startForeground(NOTIFICATION_ID, notification)

        acquireWakeLock()

        val resolvedDir = tsnetDir.ifEmpty {
            "${filesDir.absolutePath}/tsnet"
        }

        TsProxyApp.appendLog("startProxy: socks=$socks host=$hostname dir=$resolvedDir")

        Thread({
            try {
                val result = Tsproxy.start(socks, hostname, resolvedDir)
                val status = when {
                    result.startsWith("ERROR:") -> "Failed: ${result.removePrefix("ERROR: ").take(80)}"
                    else -> "Running on $socks"
                }
                TsProxyApp.appendLog("Tsproxy.start returned: $status")
                updateNotification(status)
            } catch (e: Exception) {
                val sw = java.io.StringWriter()
                e.printStackTrace(java.io.PrintWriter(sw))
                TsProxyApp.appendLog("startProxy EXCEPTION: $sw")
                Log.e(TAG, "Start failed with exception", e)
                updateNotification("Crash: ${e.message?.take(80) ?: "unknown"}")
            }
        }, "ts-proxy-start").start()
    }

    private fun stopProxy() {
        Thread({
            try {
                Tsproxy.stop()
                TsProxyApp.appendLog("stopProxy: stopped")
            } catch (e: Exception) {
                TsProxyApp.appendLog("stopProxy EXCEPTION: ${e.message}")
                Log.e(TAG, "Stop failed", e)
            } finally {
                releaseWakeLock()
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
            }
        }, "ts-proxy-stop").start()
    }

    private fun buildNotification(text: String): Notification {
        val tapIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        val pendingIntent = PendingIntent.getActivity(
            this, 0, tapIntent, PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, TsProxyApp.CHANNEL_ID)
            .setContentTitle("ts-socks5")
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_vpn_key)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .setSilent(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    private fun updateNotification(text: String) {
        try {
            val nm = getSystemService(Context.NOTIFICATION_SERVICE) as android.app.NotificationManager
            nm.notify(NOTIFICATION_ID, buildNotification(text))
        } catch (e: Exception) {
            Log.e(TAG, "Failed to update notification", e)
        }
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        // Restart service when user swipes app from recents
        val restartIntent = Intent(applicationContext, TsProxyService::class.java).apply {
            action = ACTION_START
        }
        val pendingIntent = PendingIntent.getService(
            applicationContext, 1, restartIntent,
            PendingIntent.FLAG_ONE_SHOT or PendingIntent.FLAG_IMMUTABLE
        )
        val alarmManager = getSystemService(Context.ALARM_SERVICE) as android.app.AlarmManager
        alarmManager.set(
            android.app.AlarmManager.ELAPSED_REALTIME,
            android.os.SystemClock.elapsedRealtime() + 1000,
            pendingIntent
        )
        super.onTaskRemoved(rootIntent)
    }

    override fun onDestroy() {
        releaseWakeLock()
        super.onDestroy()
    }

    companion object {
        private const val TAG = "TsProxyService"
        const val ACTION_START = "com.tsproxy.android.START"
        const val ACTION_STOP = "com.tsproxy.android.STOP"
        const val EXTRA_SOCKS = "socks"
        const val EXTRA_HOSTNAME = "hostname"
        const val EXTRA_TSNET_DIR = "tsnet_dir"
        private const val NOTIFICATION_ID = 1001

        fun start(context: Context, socks: String, hostname: String, tsnetDir: String) {
            val intent = Intent(context, TsProxyService::class.java).apply {
                action = ACTION_START
                putExtra(EXTRA_SOCKS, socks)
                putExtra(EXTRA_HOSTNAME, hostname)
                putExtra(EXTRA_TSNET_DIR, tsnetDir)
            }
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, TsProxyService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }
}
