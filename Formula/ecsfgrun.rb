class Ecsfgrun < Formula
  desc "AWS assume role credential wrapper"
  homepage "https://github.com/masahide/ecsfgrun"
  url "https://github.com/masahide/ecsfgrun/releases/download/v0.1.0/ecsfgrun_Darwin_x86_64.tar.gz"
  version "0.1.0"
  sha256 "1ff7ad71d900e90e29c4cadd4930d253a09392702af53731d71ecca6a2e50318"

  def install
    bin.install "ecsfgrun"
  end

  test do
    system "#{bin}/ecsfgrun -v"
  end
end
