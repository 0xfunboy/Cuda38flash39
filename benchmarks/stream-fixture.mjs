// A larger incremental I/O regression, prepared independently before model use.
const decoder=String.raw`#include "decoder.hpp"
#include <stdexcept>

Decoder::Decoder(std::size_t max_payload):max_payload_(max_payload) {
    if(max_payload>65535)throw std::invalid_argument("maximum exceeds wire length");
}
void Decoder::reset() noexcept {pending_.clear();}
std::size_t Decoder::buffered_bytes() const noexcept {return pending_.size();}
std::vector<std::string> Decoder::feed(std::string_view chunk) {
    pending_.append(chunk.data(),chunk.size());
    std::vector<std::string> ready;
    std::size_t offset=0;
    try {
        while(pending_.size()-offset>=2) {
            const auto length=wire_length(pending_[offset],pending_[offset+1]);
            if(length>max_payload_)throw FrameError("frame exceeds configured limit");
            const std::size_t frame_size=static_cast<std::size_t>(length)+3;
            if(pending_.size()-offset<frame_size)break;
            const std::string_view payload(pending_.data()+offset+2,length);
            const auto received=static_cast<unsigned char>(pending_[offset+2+length]);
            if(wire_checksum(payload)!=received)throw FrameError("checksum mismatch");
            ready.emplace_back(payload);
            offset+=frame_size;
        }
        pending_.erase(0,offset);
        return ready;
    } catch(const FrameError&) {
        pending_.clear();
        throw;
    }
}
`;

export const streamFixture={
  id:'07-incremental-frame-decoder',category:'incremental-io-multi-file-transaction',
  task:'Fix the incremental binary-frame decoder after its buffering refactor. Preserve the public API and wire format documented in protocol.hpp. A frame is a two-byte big-endian unsigned payload length, exactly that many arbitrary bytes, then one XOR checksum byte (XOR of payload bytes, zero for empty payload). feed accepts any chunk boundaries, including empty chunks, partial headers/bodies/checksums, and multiple complete frames followed by a partial frame. Return only complete valid payloads produced by this call, in order; retain every byte of the incomplete tail. buffered_bytes reports the retained raw wire bytes. Reject a declared payload above the constructor limit as soon as the two-byte header is available. On oversized length or checksum mismatch, throw FrameError, return no results from this call and discard ALL pending bytes, including any previous partial frame; the next feed must accept a fresh frame. reset discards buffered bytes and preserves the configured limit. Limit 0 accepts only empty payloads; limits above 65535 throw invalid_argument. No corruption resynchronization or skipped bytes is permitted. Diagnose the implementation from all relevant files, including helper byte semantics, and keep error handling and chunk boundaries consistent.',
  files:{
    'include/protocol.hpp':String.raw`#pragma once
#include <cstddef>
#include <cstdint>
#include <stdexcept>
#include <string_view>
// Wire: [length_hi:1][length_lo:1][payload:length][xor_checksum:1].
// All arithmetic is on unsigned bytes. No sentinel or escaping is used.
// Empty payloads are valid and have checksum zero; data may contain NUL.
class FrameError:public std::runtime_error {public:using std::runtime_error::runtime_error;};
std::uint16_t wire_length(char hi,char lo) noexcept;
unsigned char wire_checksum(std::string_view bytes) noexcept;
`,
    'include/decoder.hpp':String.raw`#pragma once
#include "protocol.hpp"
#include <string>
#include <vector>
class Decoder {
    std::size_t max_payload_;
    std::string pending_;
public:
    explicit Decoder(std::size_t max_payload=4096);
    std::vector<std::string> feed(std::string_view chunk);
    void reset() noexcept;
    std::size_t buffered_bytes() const noexcept;
};
`,
    'src/protocol.cpp':String.raw`#include "protocol.hpp"
std::uint16_t wire_length(char hi,char lo) noexcept {
    return static_cast<std::uint16_t>((static_cast<unsigned char>(hi)<<8)|static_cast<unsigned char>(lo));
}
unsigned char wire_checksum(std::string_view bytes) noexcept {
    unsigned char checksum=0;
    for(unsigned char byte:bytes)checksum^=byte;
    return checksum;
}
`,
    'src/decoder.cpp':decoder,
  },
  injection:['src/decoder.cpp',decoder,decoder
    .replace('if(pending_.size()-offset<frame_size)break;','if(pending_.size()-offset<frame_size){offset+=2;break;}')
    .replace('pending_.clear();\n        throw;','if(offset>0)pending_.erase(0,offset);\n        throw;')],
  test:String.raw`#include "decoder.hpp"
#include <algorithm>

// Independent encoder/oracle: does not call the production checksum/length helpers.
std::string frame(const std::string& bytes){
    std::string out;out.push_back(static_cast<char>(bytes.size()/256));out.push_back(static_cast<char>(bytes.size()%256));
    unsigned sum=0;for(char c:bytes)sum^=static_cast<unsigned char>(c);out+=bytes;out.push_back(static_cast<char>(sum));return out;
}
template<class F>bool frame_error(F fn){try{fn();}catch(const FrameError&){return true;}return false;}
int main(){
    const std::vector<std::string> expected={"alpha",std::string("a\0b\xff",4),"",std::string(300,'x'),"omega"};
    std::string stream;for(auto& p:expected)stream+=frame(p);
    CHECK(Decoder().feed(stream)==expected);
    // Every cut checks a different prefix state, not just fixed-size chunking.
    for(std::size_t cut=0;cut<=stream.size();++cut){
        Decoder d;auto result=d.feed(std::string_view(stream).substr(0,cut));
        auto empty=d.feed("");CHECK(empty.empty());
        auto tail=d.feed(std::string_view(stream).substr(cut));result.insert(result.end(),tail.begin(),tail.end());
        CHECK(result==expected);CHECK(d.buffered_bytes()==0);
    }
    for(int stride=1;stride<=19;++stride){
        Decoder d;std::vector<std::string> result;
        for(std::size_t pos=0;pos<stream.size();pos+=stride){auto got=d.feed(std::string_view(stream).substr(pos,stride));result.insert(result.end(),got.begin(),got.end());}
        CHECK(result==expected);CHECK(d.buffered_bytes()==0);
    }
    auto a=frame("abc");Decoder partial;
    CHECK(partial.feed(std::string_view(a).substr(0,2)).empty());CHECK(partial.buffered_bytes()==2);
    CHECK(partial.feed(std::string_view(a).substr(2,3)).empty());CHECK(partial.buffered_bytes()==5);
    CHECK((partial.feed(std::string_view(a).substr(5))==std::vector<std::string>{"abc"}));
    std::string bad=frame("bad");bad.back()^=1;Decoder recover;
    CHECK(frame_error([&]{recover.feed(frame("good")+bad);}));CHECK(recover.buffered_bytes()==0);
    CHECK((recover.feed(frame("fresh"))==std::vector<std::string>{"fresh"}));
    Decoder previous;CHECK(previous.feed(std::string_view(bad).substr(0,1)).empty());
    CHECK(frame_error([&]{previous.feed(std::string_view(bad).substr(1));}));CHECK(previous.buffered_bytes()==0);
    CHECK((previous.feed(frame("fresh"))==std::vector<std::string>{"fresh"}));
    Decoder small(2);CHECK(small.feed(std::string(1,'\0')).empty());CHECK(frame_error([&]{small.feed(std::string(1,'\3'));}));CHECK(small.buffered_bytes()==0);
    CHECK((small.feed(frame("ok"))==std::vector<std::string>{"ok"}));
    Decoder zero(0);CHECK((zero.feed(frame(""))==std::vector<std::string>{""}));CHECK(frame_error([&]{zero.feed(frame("a"));}));
    Decoder cleared;cleared.feed(std::string_view(a).substr(0,1));cleared.reset();CHECK(cleared.buffered_bytes()==0);CHECK((cleared.feed(a)==std::vector<std::string>{"abc"}));
    CHECK(invalid([]{Decoder bad_limit(65536);}));
    std::cout<<"PASS incremental-frame-decoder: exhaustive split/stride, binary data, rollback/recovery/limits\n";
}
`,cases:350,
};
