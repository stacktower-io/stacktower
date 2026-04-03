package deps

import "testing"

func TestInferGoRepoURL(t *testing.T) {
	tests := []struct {
		modulePath string
		want       string
	}{
		// GitHub modules
		{"github.com/spf13/cobra", "https://github.com/spf13/cobra"},
		{"github.com/gofiber/fiber/v2", "https://github.com/gofiber/fiber"},
		{"github.com/gin-gonic/gin", "https://github.com/gin-gonic/gin"},
		{"github.com/Azure/azure-sdk-for-go", "https://github.com/Azure/azure-sdk-for-go"},

		// GitLab modules
		{"gitlab.com/user/repo", "https://gitlab.com/user/repo"},
		{"gitlab.com/group/subgroup/project", "https://gitlab.com/group/subgroup"},

		// Bitbucket modules
		{"bitbucket.org/owner/repo", "https://bitbucket.org/owner/repo"},

		// golang.org/x modules → github.com/golang mirror
		{"golang.org/x/sync", "https://github.com/golang/sync"},
		{"golang.org/x/exp", "https://github.com/golang/exp"},
		{"golang.org/x/tools/gopls", "https://github.com/golang/tools"},

		// Vanity paths (not supported, need HTTP lookup)
		{"gopkg.in/yaml.v3", ""},
		{"k8s.io/client-go", ""},
		{"google.golang.org/grpc", ""},

		// Edge cases
		{"github.com/", ""},
		{"github.com/owner", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.modulePath, func(t *testing.T) {
			got := inferGoRepoURL(tt.modulePath)
			if got != tt.want {
				t.Errorf("inferGoRepoURL(%q) = %q, want %q", tt.modulePath, got, tt.want)
			}
		})
	}
}
