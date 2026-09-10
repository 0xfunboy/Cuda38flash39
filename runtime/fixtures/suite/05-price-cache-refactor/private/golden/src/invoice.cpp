#include "invoice.hpp"
#include <stdexcept>
std::int64_t InvoiceService::quote(const Cart& cart) {
    if(revision_!=catalog_.revision()) {cache_.clear();revision_=catalog_.revision();}
    auto cached=cache_.find(cart);if(cached!=cache_.end())return cached->second;
    std::int64_t total=0;
    for(const auto& [sku,n]:cart) {if(n<0)throw std::invalid_argument("quantity");total+=catalog_.price(sku)*n;}
    cache_[cart]=total;
    return total;
}
