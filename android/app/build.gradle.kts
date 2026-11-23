plugins {
    id("com.android.application")
    kotlin("android")
}
android {
    namespace = "desk.cyan.client"
    compileSdk = 34
    defaultConfig {
        applicationId = "desk.cyan.client"
        minSdk = 28
        targetSdk = 34
        versionCode = 1
        versionName = "android-dev"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }
    kotlinOptions { jvmTarget = "21" }
}
dependencies {
    implementation(project(":core"))
    implementation(files("libs/cyandesk.aar"))
}
