class Ecsfgrun < Formula
  desc "AWS assume role credential wrapper"
  homepage "https://github.com/masahide/ecsfgrun"
  url "https://github.com/masahide/ecsfgrun/releases/download/v0.4.0/ecsfgrun_Darwin_x86_64.tar.gz"
  version "0.4.0"
  sha256 "ade64be60b7b6f7ac2f7422ced4e0f4add6c2a2dd7aa230681f9c399b4493915"

  def install
    bin.install "ecsfgrun"
  end

  test do
    system "#{bin}/ecsfgrun -v"
  end
end
