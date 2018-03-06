class Ecsfgrun < Formula
  desc "AWS assume role credential wrapper"
  homepage "https://github.com/masahide/ecsfgrun"
  url "https://github.com/masahide/ecsfgrun/releases/download/v0.2.0/ecsfgrun_Darwin_x86_64.tar.gz"
  version "0.2.0"
  sha256 "37c3df421d85ef881203d9b8a3428e179b6fd3580c4b2b45a3d6a6a60b5372c7"

  def install
    bin.install "ecsfgrun"
  end

  test do
    system "#{bin}/ecsfgrun -v"
  end
end
