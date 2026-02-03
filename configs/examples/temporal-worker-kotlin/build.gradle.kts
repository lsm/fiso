plugins {
    kotlin("jvm") version "2.0.21"
    application
}

group = "io.fiso"
version = "0.1.0"

repositories {
    mavenCentral()
}

dependencies {
    implementation("io.temporal:temporal-sdk:1.27.0")
    implementation("com.fasterxml.jackson.core:jackson-databind:2.18.0")
}

kotlin {
    jvmToolchain(17)
}

application {
    mainClass.set("io.fiso.worker.MainKt")
}
