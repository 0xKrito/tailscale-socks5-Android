package com.tsproxy.android

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class TsProxyApp : Application() {
    override fun onCreate() {
        super.onCreate()
        instance = this
        createNotificationChannel()
        setupCrashHandler()
        appendLog("App started, SDK=${Build.VERSION.SDK_INT}, arch=${Build.SUPPORTED_ABIS.joinToString()}")
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "tailscale-socks5 service",
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "Tailscale proxy running"
                setShowBadge(false)
            }
            val manager = getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(channel)
        }
    }

    private fun setupCrashHandler() {
        val defaultHandler = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, throwable ->
            try {
                val sw = StringWriter()
                throwable.printStackTrace(PrintWriter(sw))
                appendLog("CRASH on thread ${thread.name}: $sw")
            } catch (_: Exception) {}
            defaultHandler?.uncaughtException(thread, throwable)
        }
    }

    companion object {
        const val CHANNEL_ID = "tsproxy_service"
        lateinit var instance: TsProxyApp
            private set

        fun appendLog(msg: String) {
            try {
                val ts = SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US).format(Date())
                val file = crashLogFile()
                file.appendText("[$ts] $msg\n")
                Log.d("TsProxy", msg)
            } catch (_: Exception) {}
        }

        fun readCrashLog(): String {
            val f = crashLogFile()
            return if (f.exists()) f.readText() else "No crash log."
        }

        fun clearCrashLog() {
            crashLogFile().delete()
        }

        private fun crashLogFile(): File {
            return File(instance.filesDir, "tsproxy_crash.log")
        }
    }
}
