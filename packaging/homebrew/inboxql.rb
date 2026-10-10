# Homebrew formula for InboxQL — a template.
#
# To publish it: create the repository techmuch/homebrew-tap, copy this file to
# Formula/inboxql.rb there, and fill in VERSION and SHA256 from the release's
# SHA256SUMS (the iql-darwin-universal.tar.gz line). Then:
#
#   brew install techmuch/tap/inboxql
#   iql setup
#   brew services start inboxql
#
# `brew services` manages a LaunchAgent of its own, so use it *instead of*
# `iql service install`, not as well: two agents would both try to serve the
# mailbox, and the second would find it already served. Update with
# `brew upgrade inboxql`; `iql update` refuses a Homebrew-installed binary so
# the two never disagree about what is installed.
class Inboxql < Formula
  desc "Email for engineers: a mailbox you can query"
  homepage "https://techmuch.github.io/InboxQL/"
  version "VERSION"
  url "https://github.com/techmuch/InboxQL/releases/download/v#{version}/iql-darwin-universal.tar.gz"
  sha256 "SHA256"
  # license: add once the repository declares one; it does not yet.

  depends_on :macos

  def install
    bin.install "iql"
  end

  # Runs `iql start --service`, which reads ~/.iql/settings.json — so run
  # `iql setup` once before starting it.
  service do
    run [opt_bin/"iql", "start", "--service"]
    keep_alive successful_exit: false
    log_path var/"log/inboxql.log"
    error_log_path var/"log/inboxql.log"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/iql version")
  end
end
