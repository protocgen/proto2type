plugins {
    kotlin("jvm") version "2.1.21"
    kotlin("plugin.serialization") version "2.1.21"
}

repositories {
    mavenCentral()
}

dependencies {
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.1")
    implementation("org.jetbrains.kotlinx:kotlinx-datetime:0.6.2")
}

// Point the main source set at the golden output directory.
sourceSets {
    main {
        kotlin {
            srcDir("../../testdata/golden/kotlin/gen")
        }
    }
}
