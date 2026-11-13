plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

/**
 * versionCode from the version: major*10000 + minor*100 + patch (0.10.1 → 1001). Release and
 * main builds of the same version get the same code and a newer version always a higher one,
 * so an installed app is never "downgraded" ("App not installed").
 */
fun versionCodeOf(name: String): Int {
    val parts = Regex("""^v?(\d+)\.(\d+)\.(\d+)""").find(name)?.groupValues ?: return 1
    return (parts[1].toInt() * 10000 + parts[2].toInt() * 100 + parts[3].toInt()).coerceAtLeast(1)
}
val appVersionName = System.getenv("VERSION_NAME") ?: "0.0.0-dev"

android {
    namespace = "net.scyde.streamero"
    compileSdk = 35
    buildToolsVersion = "35.0.0"

    defaultConfig {
        applicationId = "net.scyde.streamero"
        minSdk = 26
        targetSdk = 35
        versionCode = System.getenv("VERSION_CODE")?.toIntOrNull() ?: versionCodeOf(appVersionName)
        versionName = appVersionName
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        // Releases are signed with the project key from CI secrets, so a new .apk installs
        // over the old one. Without it (local builds) the debug key is used. A keystore that is
        // named but missing, or a CI release without one, fails the build instead of quietly
        // producing a debug-signed .apk that cannot update the installed app.
        val ks = System.getenv("ANDROID_KEYSTORE")?.ifBlank { null }
        if (ks != null && !file(ks).exists()) throw GradleException("ANDROID_KEYSTORE is set to $ks, but that file does not exist")
        if (ks == null && System.getenv("REQUIRE_RELEASE_SIGNING") == "true") throw GradleException("This release build needs the signing key (ANDROID_KEYSTORE_BASE64 secret)")
        if (ks != null) {
            create("release") {
                storeFile = file(ks)
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true }
    packaging { resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" } }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.navigation:navigation-compose:2.8.5")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.datastore:datastore-preferences:1.1.1")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.appcompat:appcompat:1.7.0")
    // Custom Tabs for single sign-on: the provider's login page runs in the user's browser
    // (password managers, existing provider session) instead of an in-app WebView.
    implementation("androidx.browser:browser:1.8.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")
    implementation("io.coil-kt:coil-compose:2.7.0")
    implementation("androidx.media3:media3-exoplayer:1.5.1")
    implementation("androidx.media3:media3-exoplayer-hls:1.5.1")
    implementation("androidx.media3:media3-ui:1.5.1")
    implementation("androidx.media3:media3-session:1.5.1")
    // Offline downloads run as WorkManager jobs (survive the app closing, network constraints).
    implementation("androidx.work:work-runtime-ktx:2.10.0")
    debugImplementation("androidx.compose.ui:ui-tooling")

    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
}
