# VHS Demo for Kino
Output demo.gif

Set FontSize 18
Set FontFamily "Hack"
Set Width 1200
Set Theme "TokyoNight"

Type "kino"
Enter
Sleep 2s

# Enter Movies Library
Enter
Sleep 1s

# Navigate down through movies
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms

# Show playlist modal
Space
Sleep 1s
Space
Sleep 500ms
Escape
Sleep 500ms

# Toggle inspector
Type "i"
Sleep 1.5s

# Navigate more with inspector open
Type "j"
Sleep 400ms
Type "j"
Sleep 1s

# Filter
Type "/"
Sleep 250ms
Type "1"
Sleep 250ms
Type "3"
Sleep 250ms
Enter
Sleep 500ms

# Navigate filtered results
Type "j"
Sleep 400ms
Type "j"
Sleep 500ms

# Go back to library column
Type "h"
Sleep 800ms

# Navigate to TV
Type "j"
Sleep 300ms
Type "j"
Sleep 300ms
Type "j"
Sleep 500ms

# Enter TV Shows
Enter
Sleep 800ms

# Navigate and drill down
Type "j"
Sleep 300ms
Type "j"
Sleep 500ms
Enter
Sleep 800ms
Enter
Sleep 800ms
Enter
Sleep 1s

# Global search
Type "f"
Sleep 500ms
Type "abys"
Sleep 1s
Enter
Sleep 2s
