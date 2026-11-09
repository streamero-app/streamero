# kotlinx.serialization keeps generated serializers
-keepattributes *Annotation*, InnerClasses
-keepclassmembers class kotlinx.serialization.json.** { *** Companion; }
-keep,includedescriptorclasses class net.scyde.streamero.**$$serializer { *; }
-keepclassmembers class net.scyde.streamero.** { *** Companion; kotlinx.serialization.KSerializer serializer(...); }
-dontwarn org.slf4j.**
