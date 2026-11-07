package audio

import "fmt"

// The PipeWire configuration files of Purified Voice. They are byte-for-byte what 0.3.x wrote:
// a differing file would make the next `voice set` see a change and restart PipeWire once for
// nothing.

// voiceConfTemplate is one filter chain per input device: its arguments are the device's
// description, the RNNoise plugin, the capture source, the virtual source's node name and the
// three VAD settings (threshold in percent, grace ms, retroactive grace ms).
const voiceConfTemplate = `context.modules = [
  { name = libpipewire-module-filter-chain
    args = {
      node.description = "%[1]s"
      media.name = "%[1]s"
      filter.graph = {
        nodes = [ { type = ladspa name = rnnoise plugin = "%[2]s" label = noise_suppressor_stereo
                    control = { "VAD Threshold (%%)" = %[5]d "VAD Grace Period (ms)" = %[6]d "Retroactive VAD Grace (ms)" = %[7]d } } ]
      }
      capture.props = {
                node.passive = true
                target.object = "%[3]s"
      }
      playback.props = {
                media.class = Audio/Source
                node.name = "%[4]s"
                node.description = "%[1]s"
                audio.position = [ FL FR ]
      }
    }
  }
]
`

// speakerConfTemplate is the optional filter on the speaker side: arguments are the plugin and
// the three VAD settings.
const speakerConfTemplate = `context.modules = [
  { name = libpipewire-module-filter-chain
    args = {
      node.description = "Purified Voice Speaker"
      filter.graph = { nodes = [ { type = ladspa name = rnnoise plugin = "%[1]s" label = noise_suppressor_stereo
                    control = { "VAD Threshold (%%)" = %[2]d "VAD Grace Period (ms)" = %[3]d "Retroactive VAD Grace (ms)" = %[4]d } } ] }
      capture.props = { media.class = Audio/Sink node.name = "truesense_pv_speaker" node.description = "Purified Voice Speaker" audio.position = [ FL FR ] }
      playback.props = { node.passive = true }
    }
  }
]
`

func voiceConf(dev voiceDevice, mode voiceMode, plugin string) string {
	return fmt.Sprintf(voiceConfTemplate, dev.description, plugin, dev.source(), dev.node(), mode.threshold, mode.grace, mode.retro)
}

func speakerConf(mode voiceMode, plugin string) string {
	return fmt.Sprintf(speakerConfTemplate, plugin, mode.threshold, mode.grace, mode.retro)
}
