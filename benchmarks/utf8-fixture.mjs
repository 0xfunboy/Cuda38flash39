// Feature implementation, not another single-line repair. Only the ASCII legacy
// implementation is model-facing; full UTF-8 golden/oracle stay private.
const golden=String.raw`#include "utf8.hpp"
#include <stdexcept>
std::u32string Utf8Decoder::feed(std::string_view bytes){
    if(closed_)throw std::logic_error("closed decoder");
    std::string input=pending_;if(!bytes.empty())input.append(bytes.data(),bytes.size());
    std::u32string output;
    std::size_t offset=0;
    while(offset<input.size()){
        const auto first=static_cast<unsigned char>(input[offset]);
        int width=0;char32_t scalar=0;
        if(first<=0x7f){width=1;scalar=first;}
        else if(first>=0xc2&&first<=0xdf){width=2;scalar=first&0x1f;}
        else if(first>=0xe0&&first<=0xef){width=3;scalar=first&0x0f;}
        else if(first>=0xf0&&first<=0xf4){width=4;scalar=first&0x07;}
        else throw Utf8Error("invalid leading byte");
        for(int j=1;j<width&&offset+static_cast<std::size_t>(j)<input.size();++j){
            const auto byte=static_cast<unsigned char>(input[offset+j]);
            if(byte<0x80||byte>0xbf)throw Utf8Error("invalid continuation");
            if(j==1&&((first==0xe0&&byte<0xa0)||(first==0xed&&byte>0x9f)||(first==0xf0&&byte<0x90)||(first==0xf4&&byte>0x8f)))throw Utf8Error("invalid scalar prefix");
            scalar=(scalar<<6)|(byte&0x3f);
        }
        if(input.size()-offset<static_cast<std::size_t>(width))break;
        output.push_back(scalar);offset+=width;
    }
    std::string remaining=input.substr(offset);
    pending_.swap(remaining);accepted_+=bytes.size();
    return output;
}
void Utf8Decoder::finish(){if(closed_)throw std::logic_error("closed decoder");if(!pending_.empty())throw Utf8Error("truncated stream");closed_=true;}
void Utf8Decoder::reset()noexcept{pending_.clear();accepted_=0;closed_=false;}
`;
const legacy=String.raw`#include "utf8.hpp"
#include <stdexcept>
std::u32string Utf8Decoder::feed(std::string_view bytes){
    if(closed_)throw std::logic_error("closed decoder");
    std::u32string output;
    for(unsigned char byte:bytes){if(byte>0x7f)throw Utf8Error("legacy ASCII only");output.push_back(byte);}
    accepted_+=bytes.size();return output;
}
void Utf8Decoder::finish(){if(closed_)throw std::logic_error("closed decoder");if(!pending_.empty())throw Utf8Error("truncated stream");closed_=true;}
void Utf8Decoder::reset()noexcept{pending_.clear();accepted_=0;closed_=false;}
`;
export const utf8Fixture={
  id:'10-incremental-utf8-feature',category:'feature-incremental-parser-scalar-transaction',
  task:'Implement full strict incremental UTF-8 decoding in the existing ASCII-only service. Preserve utf8.hpp public API and obey README.md. Deliver actual decoding logic, not a special-case patch or lookup table. No external libraries are required; arbitrary valid Unicode scalar values and arbitrary byte chunk boundaries must work.',
  files:{
    'README.md':`# Incremental UTF-8 feature

feed(bytes) returns complete decoded Unicode scalars in order and buffers an
incomplete valid prefix. Empty chunks work. NUL, BOM and noncharacters are data.
bytes_accepted counts accepted input bytes, including valid incomplete prefixes.
pending_bytes reports buffered raw bytes. Total accepted counts fit uint64_t.

Strict valid byte patterns (hex): 00-7F; C2-DF 80-BF;
E0 A0-BF 80-BF; E1-EC/EE-EF 80-BF 80-BF; ED 80-9F 80-BF;
F0 90-BF 80-BF 80-BF; F1-F3 80-BF 80-BF 80-BF;
F4 80-8F 80-BF 80-BF. Reject every other pattern with Utf8Error
as soon as invalidity is known, even in an incomplete prefix.

Malformed feed is atomic: throw and preserve ALL prior state, pending byte
contents, counters and closed flag; no partial output or acceptance. The caller
can retry that same prior prefix with correct bytes. Do not replace bad data.
finish rejects a pending incomplete scalar with Utf8Error, preserving state so
feeding may continue. Successful finish closes the decoder; later feed (even
empty) or finish throws logic_error. reset reopens and clears all state/counters.
No threading, normalization, BOM stripping or encoding-detection is in scope.
`,
    'include/utf8.hpp':String.raw`#pragma once
#include <cstdint>
#include <stdexcept>
#include <string>
#include <string_view>
class Utf8Error:public std::runtime_error{public:using std::runtime_error::runtime_error;};
class Utf8Decoder {
    std::string pending_;
    std::uint64_t accepted_=0;
    bool closed_=false;
public:
    std::u32string feed(std::string_view bytes);
    void finish();
    void reset() noexcept;
    std::uint64_t bytes_accepted()const noexcept{return accepted_;}
    std::size_t pending_bytes()const noexcept{return pending_.size();}
    bool closed()const noexcept{return closed_;}
};
`,
    'src/utf8.cpp':golden,
  },
  injection:['src/utf8.cpp',golden,legacy],allowed_paths:['src/utf8.cpp'],
  test:String.raw`#include "utf8.hpp"
#include <vector>
// Independent scalar encoder; the oracle's expected output is the original
// scalar sequence, not another decoding implementation.
std::string encode(char32_t cp){
    std::string out;
    if(cp<0x80)out+=static_cast<char>(cp);
    else if(cp<0x800){out+=static_cast<char>(0xc0|(cp>>6));out+=static_cast<char>(0x80|(cp&63));}
    else if(cp<0x10000){out+=static_cast<char>(0xe0|(cp>>12));out+=static_cast<char>(0x80|((cp>>6)&63));out+=static_cast<char>(0x80|(cp&63));}
    else{out+=static_cast<char>(0xf0|(cp>>18));out+=static_cast<char>(0x80|((cp>>12)&63));out+=static_cast<char>(0x80|((cp>>6)&63));out+=static_cast<char>(0x80|(cp&63));}
    return out;
}
std::string raw(std::initializer_list<unsigned> bytes){std::string s;for(auto byte:bytes)s+=static_cast<char>(byte);return s;}
template<class F>bool utf8_error(F fn){try{fn();}catch(const Utf8Error&){return true;}return false;}
template<class F>bool closed_error(F fn){try{fn();}catch(const std::logic_error&){return true;}return false;}
bool reject_preserving(Utf8Decoder& d,const std::string& bytes){auto n=d.bytes_accepted(),p=d.pending_bytes();auto c=d.closed();return utf8_error([&]{d.feed(bytes);})&&d.bytes_accepted()==n&&d.pending_bytes()==p&&d.closed()==c;}
int main(){try{
    std::u32string scalars={0,1,0x7f,0x80,0x7ff,0x800,0xd7ff,0xe000,0xffff,0x10000,0x10ffff,0xfeff,0xfffe,0xfdd0};
    for(unsigned i=1;i<=64;++i){char32_t cp=(i*7919u)%0x110000u;if(cp>=0xd800&&cp<=0xdfff)cp+=0x800;scalars+=cp;}
    std::string encoded;for(auto cp:scalars)encoded+=encode(cp);
    Utf8Decoder whole;CHECK(whole.feed(encoded)==scalars);CHECK(whole.bytes_accepted()==encoded.size());whole.finish();CHECK(whole.closed());
    for(std::size_t split=0;split<=encoded.size();++split){
        Utf8Decoder d;auto out=d.feed(std::string_view(encoded).substr(0,split));CHECK(d.bytes_accepted()==split);CHECK(d.feed(std::string_view{}).empty());
        out+=d.feed(std::string_view(encoded).substr(split));d.finish();CHECK(out==scalars);CHECK(d.bytes_accepted()==encoded.size()&&d.pending_bytes()==0&&d.closed());
    }
    for(std::size_t stride=1;stride<=13;++stride){Utf8Decoder d;std::u32string out;for(std::size_t pos=0;pos<encoded.size();pos+=stride)out+=d.feed(std::string_view(encoded).substr(pos,stride));d.finish();CHECK(out==scalars);}
    const std::vector<std::string> invalids={raw({0x80}),raw({0xbf}),raw({0xc0,0x80}),raw({0xc1,0xbf}),raw({0xe0,0x80}),raw({0xed,0xa0}),raw({0xf0,0x80}),raw({0xf4,0x90}),raw({0xf5}),raw({0xff}),raw({0xc2,0x20}),raw({0xe1,0x80,0x41}),raw({0xf1,0x80,0x80,0x7f})};
    for(const auto& bad:invalids){Utf8Decoder d;CHECK(d.feed("ok")==U"ok");CHECK(reject_preserving(d,"valid"+bad));CHECK(d.feed("!")==U"!");CHECK(d.bytes_accepted()==3);d.finish();}
    Utf8Decoder retry;CHECK(retry.feed(raw({0xe2})).empty());CHECK(retry.pending_bytes()==1&&retry.bytes_accepted()==1);CHECK(reject_preserving(retry,raw({0x28,0xa1})));CHECK(retry.feed(raw({0x82,0xac}))==std::u32string{0x20ac});CHECK(retry.bytes_accepted()==3&&retry.pending_bytes()==0);
    Utf8Decoder pending;CHECK(pending.feed(raw({0xf0,0x90})).empty());CHECK(utf8_error([&]{pending.finish();}));CHECK(!pending.closed()&&pending.bytes_accepted()==2&&pending.pending_bytes()==2);CHECK(reject_preserving(pending,raw({0x80,0x20})));CHECK(pending.feed(raw({0x80,0x80}))==std::u32string{0x10000});pending.finish();CHECK(pending.closed());
    CHECK(closed_error([&]{pending.feed("");}));CHECK(closed_error([&]{pending.finish();}));CHECK(pending.bytes_accepted()==4);pending.reset();CHECK(!pending.closed()&&pending.pending_bytes()==0&&pending.bytes_accepted()==0);CHECK(pending.feed("A")==U"A");pending.finish();
    Utf8Decoder reset_partial;reset_partial.feed(raw({0xe2,0x82}));reset_partial.reset();CHECK(reset_partial.feed("fresh")==U"fresh");CHECK(reset_partial.bytes_accepted()==5);
    std::cout<<"PASS incremental-utf8: 78 scalar values, every split, 13 chunk strides, strict-prefix rejection, transactional retry/finish/reset\n";
}catch(const std::exception& e){std::cerr<<"FAIL valid UTF-8 or lifecycle rejected: "<<e.what()<<'\n';return 1;}}
`,cases:78,
};
