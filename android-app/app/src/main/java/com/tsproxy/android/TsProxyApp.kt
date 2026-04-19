package com.tsproxy.android

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.os.Build
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
                "ts-proxy service",
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "Tailscale proxy running notification"
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
            // Delegate to default handler so the app still terminates
            defaultHandler?.uncaughtException(thread, throwable)
        }
    }

    companion object {
        const val CHANNEL_ID = "tsproxy_service"
        lateinit var instance: TsProxyApp
            private set

        /** Append a line to the crash/log file. */
        fun appendLog(msg: String) {
            try {
                val ts = SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US).format(Date())
                val file = crashLogFile()
                file.appendText("[$ts] $msg\n")
                // Also log to logcat
                Log.d("TsProxy", msg)
            } catch (_: Exception) {}
        }

        /** Read all crash logs. */
        fun readCrashLog(): String {
            val f = crashLogFile()
            return if (f.exists()) f.readText() else "No crash log."
        }

        /** Clear crash log. */
        fun clearCrashLog() {
            crashLogFile().delete()
        }

        /** Path: /data/data/com.tsproxy.android/files/tsproxy_crash.log */
        private fun crashLogFile(): File {
            return File(instance.filesDir, "tsproxy_crash.log")
        }
    }
}
